package japanese_test

import (
	"testing"

	"github.com/aravinditte/benri-agent/internal/japanese"
)

func TestStyleAcceptsPoliteBusinessDraft(t *testing.T) {
	body := "お問い合わせありがとうございます。配送状況は「配送中」となっております。ご不明な点がございましたら、お気軽にお問い合わせください。\nひなたストア カスタマーサポート"
	res := japanese.CheckStyle(body)
	if !res.OK {
		t.Errorf("polite draft rejected: %v", res.Violations)
	}
}

func TestStyleRejectsCasualAndBanned(t *testing.T) {
	body := "それは無理だよ。必ず返金します。\nひなたストア カスタマーサポート"
	res := japanese.CheckStyle(body)
	if res.OK {
		t.Error("casual/banned draft must fail style checks")
	}
	missing := japanese.CheckStyle("ご連絡ありがとうございます。確認いたします。")
	if missing.OK {
		t.Error("draft without the required signature must fail")
	}
}
