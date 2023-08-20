package config

const Name = "order-svc"

var (
	Port         = "8080"
	Db_host      = "127.0.0.1:4317"
	Otel_host    = "127.0.0.1"
	Db_max_conn  = "80"
	Sampler      = float64(1)
	Payment_host = "127.0.0.1"
)
