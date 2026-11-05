package server

import (
	"context"
)

type Server interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type Servers struct {
	servers []Server
}

func NewServersProvider(servers ...Server) *Servers {
	return &Servers{servers: servers}

}

func (s *Servers) Start(ctx context.Context) error {
	for _, srv := range s.servers {
		if err := srv.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (s *Servers) Stop(ctx context.Context) error {
	for _, srv := range s.servers {
		if err := srv.Stop(ctx); err != nil {
			return err
		}
	}

	return nil
}
