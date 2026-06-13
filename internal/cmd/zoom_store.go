package cmd

import (
	"context"
	"os"

	"github.com/steipete/gogcli/internal/app"
	"github.com/steipete/gogcli/internal/config"
	"github.com/steipete/gogcli/internal/secrets"
	"github.com/steipete/gogcli/internal/zoom"
)

func commandZoomStore(ctx context.Context) (*zoom.Store, error) {
	if runtime, ok := app.FromContext(ctx); ok {
		if err := configureRuntimeLayout(runtime, "", config.PathKindConfig); err != nil {
			return nil, err
		}
		store, err := zoom.NewStore(runtime.Layout, func() (secrets.SecretStore, error) {
			if runtime.Auth.OpenSecretStore != nil {
				return runtime.Auth.OpenSecretStore()
			}
			if err := configureRuntimeSecrets(runtime, ""); err != nil {
				return nil, err
			}
			return secrets.OpenWithConfig(runtime.Layout, runtime.Config)
		}, os.LookupEnv)
		if err != nil {
			return nil, err
		}
		return store, nil
	}

	layout, err := config.ResolveSystemLayoutFor("", config.PathKindConfig)
	if err != nil {
		return nil, err
	}
	store, err := zoom.NewStore(layout, func() (secrets.SecretStore, error) {
		fullLayout, layoutErr := config.ResolveSystemLayoutFor("", config.PathKindConfig, config.PathKindData)
		if layoutErr != nil {
			return nil, layoutErr
		}
		return secrets.OpenWithConfig(fullLayout, config.NewConfigStore(fullLayout))
	}, os.LookupEnv)
	if err != nil {
		return nil, err
	}
	return store, nil
}
