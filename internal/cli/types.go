package cli

import (
	"context"
	"io"
	"os"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/app"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

type Mode string

const (
	ModeMeta   Mode = "meta"
	ModeLegacy Mode = "legacy"
)

type Gateway interface {
	Status(context.Context, app.RequestOptions) (ipgw.Status, error)
	Login(context.Context, app.LoginOptions) (ipgw.LoginResult, error)
	Logout(context.Context, app.RequestOptions) (ipgw.LogoutResult, error)
	ListInterfaces(context.Context) ([]ipgw.Interface, error)
}

type GatewayFactory func(config.Paths) Gateway

type Options struct {
	Mode            Mode
	Args            []string
	Paths           config.Paths
	ResolvePaths    func() (config.Paths, error)
	NewGateway      GatewayFactory
	Input           *os.File
	Out             io.Writer
	Err             io.Writer
	IsTTY           bool
	Version         string
	ProviderOptions *config.ProviderOptions
}

type globalOptions struct {
	output     outputMode
	configPath string
	profile    string
	bindIP     string
	version    bool
}

type outputMode string

const (
	outputHuman outputMode = "human"
	outputJSON  outputMode = "json"
)
