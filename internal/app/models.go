// Copyright 2026 The LiquidHarness Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/HugoFernandoColmenares/LiquidHarness/internal/core"
	"github.com/HugoFernandoColmenares/LiquidHarness/internal/models"
)

const manifestTimeout = 60 * time.Second

// ModelListResult is the stable output of `models list`.
type ModelListResult struct {
	Dir     string         `json:"dir"`
	Current string         `json:"current,omitempty"`
	Models  []models.Model `json:"models"`
}

// ModelVerifyReport is the stable output of `models verify`.
type ModelVerifyReport struct {
	Dir     string                `json:"dir"`
	OK      bool                  `json:"ok"`
	Results []models.VerifyResult `json:"results"`
}

// ModelPathResult is the stable output of `models path`.
type ModelPathResult struct {
	Dir  string `json:"dir"`
	ID   string `json:"id,omitempty"`
	Path string `json:"path"`
}

// ModelManager builds the model cache manager for the current configuration.
func (a *App) ModelManager(ctx context.Context) (*models.Manager, error) {
	if !a.configured {
		return nil, core.E(core.CodeInternal, "configuration was not loaded")
	}

	client := &http.Client{Timeout: manifestTimeout}
	manifest, err := models.LoadManifest(ctx, a.Config.Model.Manifest, client)
	if err != nil {
		return nil, err
	}

	return models.New(models.Options{
		Dir:      a.Paths.ModelsDir,
		Manifest: manifest,
		Mirror:   a.Config.Model.Mirror,
		Progress: a.progressWriter(),
	})
}

func (a *App) progressWriter() io.Writer {
	if a.JSON {
		return nil
	}
	return a.Stderr
}

// ModelList reports every advertised model and any installed model no longer in the manifest.
func (a *App) ModelList(ctx context.Context) (ModelListResult, error) {
	manager, err := a.ModelManager(ctx)
	if err != nil {
		return ModelListResult{}, err
	}
	list, err := manager.List()
	if err != nil {
		return ModelListResult{}, err
	}
	current, err := manager.Current()
	if err != nil {
		return ModelListResult{}, err
	}
	if list == nil {
		list = []models.Model{}
	}
	return ModelListResult{Dir: manager.Dir(), Current: current, Models: list}, nil
}

// ModelPull downloads and installs one model.
func (a *App) ModelPull(ctx context.Context, id string, acceptLicense bool) (models.Model, error) {
	manager, err := a.ModelManager(ctx)
	if err != nil {
		return models.Model{}, err
	}
	return manager.Pull(ctx, id, models.PullOptions{AcceptLicense: acceptLicense})
}

// ModelVerify recomputes the checksum of one installed model, or of every installed model when
// id is empty. A mismatch is reported in the result; only I/O failures are errors.
func (a *App) ModelVerify(ctx context.Context, id string) (ModelVerifyReport, error) {
	manager, err := a.ModelManager(ctx)
	if err != nil {
		return ModelVerifyReport{}, err
	}

	var results []models.VerifyResult
	if strings.TrimSpace(id) == "" {
		results, err = manager.VerifyAll(ctx)
	} else {
		var result models.VerifyResult
		result, err = manager.Verify(ctx, id)
		results = []models.VerifyResult{result}
	}
	if err != nil {
		return ModelVerifyReport{}, err
	}
	if results == nil {
		results = []models.VerifyResult{}
	}

	report := ModelVerifyReport{Dir: manager.Dir(), OK: true, Results: results}
	for _, result := range results {
		if !result.OK {
			report.OK = false
			break
		}
	}
	return report, nil
}

// ModelUse selects an installed model as the default for autostart.
func (a *App) ModelUse(ctx context.Context, id string) (models.Model, error) {
	manager, err := a.ModelManager(ctx)
	if err != nil {
		return models.Model{}, err
	}
	return manager.Use(id)
}

// ModelRemove deletes one installed model.
func (a *App) ModelRemove(ctx context.Context, id string, force bool) error {
	manager, err := a.ModelManager(ctx)
	if err != nil {
		return err
	}
	return manager.Remove(id, force)
}

// ModelPath returns the cache directory when id is empty, or the GGUF path of one installed
// model. With no id it also reports the current model when one is selected.
func (a *App) ModelPath(ctx context.Context, id string) (ModelPathResult, error) {
	manager, err := a.ModelManager(ctx)
	if err != nil {
		return ModelPathResult{}, err
	}
	result := ModelPathResult{Dir: manager.Dir()}

	id = strings.TrimSpace(id)
	if id == "" {
		current, err := manager.Current()
		if err != nil {
			return ModelPathResult{}, err
		}
		if current == "" {
			result.Path = manager.Dir()
			return result, nil
		}
		id = current
	}

	installed, err := manager.Installed(id)
	if err != nil {
		return ModelPathResult{}, err
	}
	result.ID = installed.ID
	result.Path = installed.Path
	return result, nil
}

// currentModelPath resolves the GGUF selected by `models use`. It uses only the embedded
// manifest, so autostart never performs a network request to find a local file.
func (a *App) currentModelPath() (string, error) {
	if !a.configured {
		return "", core.E(core.CodeInternal, "configuration was not loaded")
	}
	manifest, err := models.DefaultManifest()
	if err != nil {
		return "", err
	}
	manager, err := models.New(models.Options{
		Dir:      a.Paths.ModelsDir,
		Manifest: manifest,
	})
	if err != nil {
		return "", err
	}

	current, err := manager.Current()
	if err != nil {
		return "", err
	}
	if current == "" {
		return "", core.E(core.CodeNotFound, "no model is selected; run liquidharness models use <id>")
	}
	installed, err := manager.Installed(current)
	if err != nil {
		return "", err
	}
	return installed.Path, nil
}
