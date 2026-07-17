package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"relay/internal/store"
)

// An inbound message's detail must surface its webhook delivery history and the
// SPF/DKIM results — not just the (outbound-only) delivery attempts.
func TestGetMessageInboundWebhookDeliveries(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()

	did := uuid.MustParse(createDomainForTest(t, ts.URL, testToken, "inbound-detail.example"))
	mb, err := testStore.CreateMailbox(ctx, store.CreateMailboxParams{
		DomainID: did, LocalPart: "support", WebhookUrl: "https://app.example/inbound", WebhookSecretEnc: []byte("x"),
	})
	if err != nil {
		t.Fatal(err)
	}

	pass := "pass"
	mid := uuid.New()
	if _, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: mid, Direction: "inbound", DomainID: &did,
		MailFrom: strPtr("a@ext.example"), HeaderFrom: strPtr("a@ext.example"),
		RcptTo: []string{"support@inbound-detail.example"}, Status: "received",
		SpfResult: &pass, DkimResult: &pass,
	}); err != nil {
		t.Fatal(err)
	}
	wd, err := testStore.CreateWebhookDelivery(ctx, store.CreateWebhookDeliveryParams{MailboxID: mb.ID, MessageID: mid})
	if err != nil {
		t.Fatal(err)
	}
	if err := testStore.MarkWebhookSuccess(ctx, store.MarkWebhookSuccessParams{ID: wd.ID, StatusCode: i32ptr(200), ResponseSnippet: strPtr("ok")}); err != nil {
		t.Fatal(err)
	}

	status, out := do(t, "GET", ts.URL+"/v1/messages/"+mid.String(), testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get message = %d (%v)", status, out)
	}
	msg := out["message"].(map[string]any)
	if msg["spf_result"] != "pass" || msg["dkim_result"] != "pass" {
		t.Errorf("spf/dkim results not surfaced: spf=%v dkim=%v", msg["spf_result"], msg["dkim_result"])
	}
	whs, ok := out["webhook_deliveries"].([]any)
	if !ok || len(whs) != 1 {
		t.Fatalf("expected 1 webhook delivery, got %v", out["webhook_deliveries"])
	}
	first := whs[0].(map[string]any)
	if first["result"] != "success" || first["status_code"].(float64) != 200 {
		t.Errorf("webhook delivery not reported correctly: %v", first)
	}
}

func i32ptr(n int32) *int32 { return &n }
