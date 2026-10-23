package userbox

const (
	allowed = "UPDATE user_state SET pts = ? WHERE user_id = ?"
	events  = "INSERT INTO mq_outbox (topic) VALUES (?)"
)
