package postgres

type Option func(*Postgres)

func WithBeforeConnectCallbacks(callbacks ...BeforeConnectCallback) Option {
	return func(p *Postgres) {
		p.beforeConnectCallbacks = callbacks
	}
}

func WithAfterConnectCallbacks(callbacks ...AfterConnectCallback) Option {
	return func(p *Postgres) {
		p.afterConnectCallbacks = callbacks
	}
}
