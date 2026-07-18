import { api } from "../lib/api";

export interface DmarcSummary {
  total: number;
  passed: number;
  dkim_pass: number;
  spf_pass: number;
  quarantined: number;
  rejected: number;
}

export interface DmarcSource {
  source_ip: string;
  total: number;
  passed: number;
}

export interface DmarcReport {
  org_name: string;
  report_id: string;
  date_begin: string | null;
  date_end: string | null;
  policy_p: string | null;
  policy_pct: number | null;
  messages: number;
  received_at: string | null;
}

export interface DmarcAnalysis {
  window: string;
  summary: DmarcSummary;
  top_sources: DmarcSource[];
  reports: DmarcReport[];
}

export function getDMARC(id: string, window = "30d"): Promise<DmarcAnalysis> {
  return api(`/v1/domains/${id}/dmarc?window=${window}`);
}
