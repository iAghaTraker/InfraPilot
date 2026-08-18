// Package modules defines the registry contract for optional InfraPilot modules.
package modules

import "context"

type Status string

const (
	StatusAvailable Status = "available"
	StatusInstalled Status = "installed"
	StatusDisabled  Status = "disabled"
)

type Module interface {
	Name() string
	Version() string
	Status(context.Context) (Status, error)
	Install(context.Context) error
	Remove(context.Context) error
}

type Registry struct{ items []Module }

func NewRegistry(items ...Module) *Registry { return &Registry{items: append([]Module(nil), items...)} }
func (r *Registry) List() []Module          { return append([]Module(nil), r.items...) }
