package debugger

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"

	"github.com/polarismesh/polaris-sidecar/pkg/constants"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

type DebugServer struct {
	svr  *http.Server
	bind string
	port int32
	once sync.Once
}

const DefaultListenPort = 50000

func NewDebugServer(bind string, port int32) *DebugServer {
	return &DebugServer{
		bind: bind,
		port: port,
		svr: &http.Server{
			Handler: http.NewServeMux(),
		},
	}
}

func (s *DebugServer) Run(ctx context.Context, wg *sync.WaitGroup, errChan chan error) {
	if s == nil {
		log.Info("[debug-server] debug server is nil, skip run")
		return
	}
	wg.Add(1)
	log.Info("[debug-server] start debug server")
	defer func() {
		wg.Done()
		s.Destroy()
	}()
	ln, err := net.Listen(constants.TcpProtocol, fmt.Sprintf("%s:%d", s.bind, s.port))
	if err != nil {
		log.Errorf(": %v", err)
		errChan <- err
		return
	}
	mux, ok := s.svr.Handler.(*http.ServeMux)
	if !ok {
		log.Errorf("[debug-server] debug server handler is not debugger.ServeMux")
		errChan <- fmt.Errorf("debug server handler is not debugger.ServeMux")
		return
	}
	mux.HandleFunc("/sidecar/health/readiness", func(resp http.ResponseWriter, _ *http.Request) {
		resp.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/sidecar/health/liveness", func(resp http.ResponseWriter, _ *http.Request) {
		resp.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	go func() {
		errChan <- s.svr.Serve(ln)
	}()

	<-ctx.Done()
	log.Infof("[debug-server] get context cancel signal, return")
}

// Destroy 退出前关闭服务（确保幂等性）
func (s *DebugServer) Destroy() {
	s.once.Do(func() {
		if s.svr != nil {
			// 创建带超时的上下文
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// 先尝试优雅关闭
			if err := s.svr.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Errorf("[debug-server] graceful shutdown failed: %v", err)

				// 优雅关闭失败时强制关闭
				if err := s.svr.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Errorf("[debug-server] force close failed: %v", err)
				}
			}
		}
		log.Infof("[debug-server] destroy debug server")
	})
}

// RegisterDebugHandler 注册dnsServer调试处理函数
func (s *DebugServer) RegisterDebugHandler(handlers []DebugHandler) error {
	mux, ok := s.svr.Handler.(*http.ServeMux)
	if !ok {
		err := fmt.Errorf("debug server handler is not debugger.ServeMux")
		log.Errorf("[debug-server] RegisterDebugHandler failed: %v", err)
		return err
	}
	for i := range handlers {
		handler := handlers[i]
		if len(handler.Path) == 0 {
			log.Infof("[debug-server] handler path is empty, skip handler: %v", handler)
			continue
		}
		mux.HandleFunc(handler.Path, handler.Handler)
	}
	return nil
}
