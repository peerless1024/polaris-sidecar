package mtls

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"

	"google.golang.org/grpc"

	caclient2 "github.com/polarismesh/polaris-sidecar/internal/mesh/mtls/certificate/caclient"
	manager2 "github.com/polarismesh/polaris-sidecar/internal/mesh/mtls/certificate/manager"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/mtls/rotator"
	"github.com/polarismesh/polaris-sidecar/internal/mesh/mtls/sds"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

type Agent struct {
	network     string
	addr        string
	ln          net.Listener
	grpcSvr     *grpc.Server
	sds         *sds.Server
	client      manager2.CSRClient
	certManager manager2.Manager
	rotator     *rotator.Rotator
	once        sync.Once
}

const defaultCAPath = "/etc/polaris-sidecar/certs/rootca.pem"

func New(opt Option) (*Agent, error) {
	err := opt.init()
	if err != nil {
		log.Errorf("[envoy-mtls] init option failed: %v", err)
		return nil, err
	}
	a := &Agent{}
	a.network = opt.Network
	a.addr = opt.Address
	a.grpcSvr = grpc.NewServer()
	a.rotator = rotator.New(opt.RotatePeriod, opt.FailedRetryDelay)
	a.sds = sds.New(opt.CryptombPollDelay)

	if opt.Network == "unix" {
		if err := os.MkdirAll(filepath.Dir(opt.Address), os.ModePerm); err != nil {
			log.Errorf("[envoy-mtls] create unix socket dir failed: %v", err)
			return nil, err
		}
	}

	cli, err := caclient2.NewWithRootCA(opt.CAServer, caclient2.ServiceAccountToken(), defaultCAPath)
	if err != nil {
		log.Errorf("[envoy-mtls] create ca polaris failed: %v", err)
		return nil, err
	}
	a.client = cli

	a.certManager = manager2.NewManager(opt.Namespace, opt.ServiceAccount, opt.RSAKeyBits, opt.TTL, a.client)
	return a, nil
}

func (a *Agent) Run(ctx context.Context, errChan chan error) {
	log.Info("[envoy-mtls] start mtls agent")
	defer func() {
		a.Destroy()
	}()
	// start sds grpc service
	a.sds.Serve(a.grpcSvr)
	l, err := net.Listen(a.network, a.addr)
	if err != nil {
		log.Errorf("[envoy-mtls] create sds grpc service listener failed: %v", err)
		errChan <- err
		return
	}
	a.ln = l
	serveErr := make(chan error, 1)
	go func() {
		err = a.grpcSvr.Serve(l)
		if err != nil {
			log.Errorf("[envoy-mtls] start sds grpc service failed: %v", err)
			serveErr <- err
			return
		}
	}()
	log.Info("[envoy-mtls] start rotator")
	// start certificate generation rotator
	if err = a.rotator.Run(ctx, func(ctx context.Context) error {
		bundle, err := a.certManager.GetBundle(ctx)
		if err != nil {
			log.Errorf("[envoy-mtls] get certificate bundle failed: %v", err)
			return err
		}
		a.sds.UpdateSecrets(ctx, *bundle)
		return nil
	}); err != nil {
		log.Errorf("[envoy-mtls] start rotator failed: %v", err)
		errChan <- err
		return
	}

	for {
		select {
		case sErr := <-serveErr:
			select {
			case errChan <- sErr: // 尝试发送
			default: // 通道满时记录日志
				log.Errorf("[envoy-mtls] error channel full, drop error: %v", sErr)
			}
			return
		case <-ctx.Done():
			log.Infof("[envoy-mtls] receive stop signal")
			return
		}
	}
}

// Destroy stop the agent
func (a *Agent) Destroy() {
	a.once.Do(func() {
		if a.grpcSvr != nil {
			a.grpcSvr.GracefulStop()
		}
		if a.ln != nil {
			if err := a.ln.Close(); err != nil {
				log.Errorf("[envoy-mtls] close listener failed: %v", err)
			}
			a.ln = nil // 置空引用避免重复关闭
		}
		log.Info("[envoy-mtls] stop and return")
	})
}
