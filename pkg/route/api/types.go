package api

import (
	kubeapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
)

// Route encapsulates the inputs needed to connect a DNS/alias to a service proxy.
type Route struct {
	kubeapi.JSONBase         `json:",inline" yaml:",inline"`

	// Required: Alias/DNS that points to the service
	// Can be host or host:port
	// host and port are combined to follow the net/url URL struct
	Host string              `json:"host" yaml:"host"`
	// Optional: Path that the router watches for, to route traffic for to the service
	Path string              `json:"path,omitempty" yaml:"path,omitempty"`

	// the name of the service that this route points to
	ServiceName string       `json:"serviceName" yaml:"serviceName"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// RouteList is a collection of Routes.
type RouteList struct {
	kubeapi.JSONBase `json:",inline" yaml:",inline"`
	Items []Route    `json:"items,omitempty" yaml:"items,omitempty"`
}
