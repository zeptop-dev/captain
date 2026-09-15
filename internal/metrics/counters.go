package metrics

// Process-wide counters the handlers and jobs bump; the server registers
// them with the registry it serves.
var (
	PaymentCallbackErrors = &Counter{Name: "captain_payment_callback_errors_total"}
	PaymentSettleErrors   = &Counter{Name: "captain_payment_settle_errors_total"}
	JobErrors             = &Counter{Name: "captain_job_errors_total"}
	NodeReports           = &Counter{Name: "captain_node_reports_total"}
	NodeStateBuilds       = &Counter{Name: "captain_node_state_builds_total"}
)
