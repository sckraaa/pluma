package outbox

const (
	events = "DELETE FROM mq_outbox WHERE id = ?"
	state  = "UPDATE user_state SET pts = 0" // not here
)
