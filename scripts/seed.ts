import { BenriStore, seedStore, seedInquiries } from "@benri/db";
import { redactPII } from "@benri/agent-core";
const path = process.env.BENRI_DB_PATH ?? "./data/benri.sqlite";
const store = new BenriStore(path, "full");
const counts = seedStore(store);
const n = seedInquiries(store, (t: string) => redactPII(t).text);
console.log(JSON.stringify({ db: path, ...counts, inquiries: n }));
store.close();
