package core

import (
	"context"
	"github.com/iAghaTraker/InfraPilot/internal/service"
	"github.com/iAghaTraker/InfraPilot/internal/service/systemd"
)

func ServiceManager() *service.Manager { return service.New(systemd.New()) }

func Services(ctx context.Context) ([]service.Service, error) { return ServiceManager().List(ctx) }
