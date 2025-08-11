package bootstrap

import (
	"context"
	"os"
	"os/signal"

	"github.com/polarismesh/polaris-sidecar/internal/bootstrap/config"
	"github.com/polarismesh/polaris-sidecar/internal/bootstrap/system"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// Start the main agent routines
func Start(configFilePath string, bootConfig *config.BootConfig) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[bootstrap] agent panic recovered: %v", r)
		}
	}()
	agent, err := initAgent(configFilePath, bootConfig)
	if err != nil {
		log.Errorf("[bootstrap] fail to init sidecar server, err: %v", err)
		os.Exit(-1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error)
	go func() {
		err = agent.runServices(ctx)
		if nil != err {
			log.Errorf("[bootstrap] agent return for err: %v", err)
			errCh <- err
		}
	}()
	runMainLoop(cancel, errCh)
}

// RunMainLoop sidecar server main loop
func runMainLoop(cancel context.CancelFunc, errCh chan error) {
	ch := make(chan os.Signal, 1)
	defer func() {
		signal.Stop(ch)
		if r := recover(); r != nil {
			log.Errorf("[bootstrap] catch panic: %v", r)
		}
		log.Infof("[bootstrap] sink logs and stop sidecar server")
		_ = log.Sync()
	}()
	signal.Notify(ch, system.Signals...)
	for {
		select {
		case s := <-ch:
			log.Infof("[bootstrap] catch signal(%+v), stop sidecar server", s)
			cancel()
			return
		case err := <-errCh:
			log.Errorf("[bootstrap] main loop return for catch err: %s", err.Error())
			cancel()
			return
		}
	}
}
