package agent

import (
	"strings"

	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/japanese"
)

// Deterministic keyword classifier used by the MOCK LLM responder (ported
// from v2). In live mode the model returns the same JSON shape; this
// classifier is the mock's implementation and is labelled as such.
// Confidence is a routing signal only, not a calibrated probability (§5.4).

var keywords = map[domain.Intent][]string{
	domain.IntentDeliveryStatus: {"配送", "配達", "届き", "届く", "発送", "追跡", "荷物", "配送状況", "いつ届", "送料"},
	domain.IntentReturnExchange: {"返品", "交換", "返送", "返金手続", "サイズが合わない", "サイズが違う", "へんぴん"},
	domain.IntentInvoiceRequest: {"請求書", "領収書", "インボイス", "適格請求書"},
	domain.IntentProductUsage:   {"使い方", "使用方法", "お手入れ", "洗い方", "設定方法", "説明書", "フィルター", "取り付け", "取り外し", "掃除方法", "保証"},
	domain.IntentCancellation:   {"キャンセル", "解約", "注文を取り消", "取り消し", "やめたい", "キャンセルしたい"},
	domain.IntentPriceStock:     {"在庫", "価格", "値段", "定価", "再入荷", "売り切れ", "割引", "ざいこ"},
	domain.IntentAccount:        {"アカウント", "会員登録", "ログイン", "会員情報", "パスワード", "住所変更", "連絡先", "電話番号", "ポイント"},
}

var ComplaintKeywords = []string{"クレーム", "苦情", "不満", "最悪", "ひどい", "謝罪", "ふざけ", "怒って"}
var LegalSafetyKeywords = []string{"訴訟", "弁護士", "裁判", "怪我", "けが", "ケガ", "病院", "消費者センター", "健康被害", "倒れ"}
var CredentialDisclosurePatterns = []string{"パスワードを教えて", "管理者のパスワード", "認証情報を開示", "内部の認証情報"}
var IdentityKeywords = []string{"パスワードを変更", "パスワード変更", "パスワードを変え", "住所変更", "住所を変更", "登録情報を変更", "メールアドレスを変更", "電話番号を変更", "氏名変更", "本人確認書類"}
var InjectionPatterns = []string{"指示を無視", "システムの指示", "ignore", "システムプロンプト", "内部指示", "権限を与え", "管理者として", "この指示に従い", "承認なしで", "ツール出力です", "ツール出力に従い"}
var PIIFishingPatterns = []string{"他のお客様の電話番号", "他のお客様の住所", "他人の電話番号", "他の顧客の", "別のお客様の個人情報"}
var UnsupportedRequestPatterns = []string{"割引して", "割引コード", "値引きして", "無料にして", "特別に返金", "保証期間を延長して", "割引を約束", "割引があると", "存在しない", "断言して", "でっち上げ", "必ず届く", "顧客情報を教え", "注文一覧を見せて"}

type Classification struct {
	Intent     domain.Intent `json:"intent"`
	Urgency    string        `json:"urgency"`
	Confidence float64       `json:"confidence"`
	Matched    []string      `json:"matched"`
}

func hits(text string, list []string) []string {
	out := []string{}
	for _, k := range list {
		if strings.Contains(text, k) {
			out = append(out, k)
		}
	}
	return out
}
func HasAny(text string, list []string) bool {
	for _, k := range list {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

func ClassifyText(raw string) Classification {
	text := japanese.Normalize(raw)
	matched := []string{}
	if h := hits(text, ComplaintKeywords); len(h) > 0 {
		return Classification{Intent: domain.IntentComplaint, Urgency: "high", Confidence: 0.95, Matched: h}
	}
	if h := hits(text, LegalSafetyKeywords); len(h) > 0 {
		return Classification{Intent: domain.IntentComplaint, Urgency: "high", Confidence: 0.9, Matched: h}
	}
	best := domain.IntentOther
	var bestHits []string
	bestScore := 0
	for _, intent := range []domain.Intent{domain.IntentDeliveryStatus, domain.IntentReturnExchange, domain.IntentInvoiceRequest, domain.IntentProductUsage, domain.IntentCancellation, domain.IntentPriceStock, domain.IntentAccount} {
		h := hits(text, keywords[intent])
		score := 0
		for _, k := range h {
			score += strings.Count(text, k)
		}
		if score > bestScore {
			best, bestHits, bestScore = intent, h, score
		}
	}
	matched = append(matched, bestHits...)
	if bestScore == 0 && (strings.Contains(text, "支払い") || strings.Contains(text, "代金引換") || strings.Contains(text, "コンビニ払い") || strings.Contains(text, "銀行振込")) {
		return Classification{Intent: domain.IntentOther, Urgency: "normal", Confidence: 0.8, Matched: []string{"支払い"}}
	}
	urgency := "normal"
	if strings.Contains(text, "至急") || strings.Contains(text, "急ぎ") || strings.Contains(text, "早急") || strings.Contains(text, "困っています") || strings.Contains(text, "困って") {
		urgency = "high"
	} else if strings.Contains(text, "暇なとき") || strings.Contains(text, "急ぎません") {
		urgency = "low"
	}
	confidence := 0.3
	if bestScore >= 2 {
		confidence = 0.92
	} else if bestScore == 1 {
		confidence = 0.78
	}
	return Classification{Intent: best, Urgency: urgency, Confidence: confidence, Matched: matched}
}
