package bootstrap

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"time"

	"github.com/polarismesh/polaris-sidecar/internal/bootstrap/config"
	"github.com/polarismesh/polaris-sidecar/internal/bootstrap/system"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

// Start the main agent routines
func Start(configFilePath string, bootConfig *config.BootConfig) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Errorf("[bootstrap] bootstrap panic recovered: %v\nStack trace:\n%s", r, string(stack))
		}
	}()
	agent, err := initAgent(configFilePath, bootConfig)
	if err != nil {
		log.Errorf("[bootstrap] fail to init sidecar server, err: %v", err)
		os.Exit(-1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := agent.getErrorChannel()
	wg := &sync.WaitGroup{}
	agent.runServices(ctx, wg, errCh)
	runMainLoop(cancel, errCh)
	<-ctx.Done()
	log.Info("[bootstrap] sidecar server start shutdown")
	// 等待所有组件完成关闭
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	// 等待30秒钟，如果所有组件都未能正常关闭，则强制退出
	select {
	case <-done:
		log.Infof("[bootstrap] all components shutdown gracefully")
	case <-time.After(30 * time.Second):
		log.Warnf("[bootstrap] graceful shutdown timed out, forcing exit")
	}
}

// RunMainLoop sidecar server main loop
func runMainLoop(cancel context.CancelFunc, errCh chan error) {
	ch := make(chan os.Signal, 1)
	defer func() {
		signal.Stop(ch)
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Errorf("[bootstrap] bootstrap panic recovered: %v\nStack trace:\n%s", r, string(stack))
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
