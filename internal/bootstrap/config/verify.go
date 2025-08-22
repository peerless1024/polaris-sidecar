package config

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"github.com/polarismesh/polaris-sidecar/internal/resolver/common"
	"github.com/polarismesh/polaris-sidecar/pkg/log"
)

const (
	areaRegion = "region"
	areaZone   = "zone"
	areaCampus = "campus"
)

var stringToMatchLevel = map[string]bool{
	areaRegion: true,
	areaZone:   true,
	areaCampus: true,
}

var stringToLevel = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
	"fatal": true,
}

func (s *SidecarConfig) verify() error {
	var errs multierror.Error
	// log
	if s.Logger.OutputLevel != "" {
		if _, ok := stringToLevel[s.Logger.OutputLevel]; !ok {
			errs.Errors = append(errs.Errors, fmt.Errorf("logger.output-level should be one of debug, info, warn, "+
				"error, fatal"))
		}
	}
	if s.Logger.RotationMaxAge < 0 || s.Logger.RotationMaxBackups < 0 || s.Logger.RotationMaxSize < 0 {
		errs.Errors = append(errs.Errors, fmt.Errorf("logger.rotation-max-age, logger.rotation-max-backups, "+
			"logger.rotation-max-size should be greater than 0"))
	}
	// nearby route match level
	if s.PolarisConfig.NearbyMatchLevel != "" {
		if _, ok := stringToMatchLevel[s.PolarisConfig.NearbyMatchLevel]; !ok {
			errs.Errors = append(errs.Errors, fmt.Errorf("polaris.nearby-match-level should be one of region, "+
				"zone, campus"))
		}
	}
	if len(s.Bind) == 0 {
		errs.Errors = append(errs.Errors, fmt.Errorf("host should not empty"))
	}
	if s.Port <= 0 {
		errs.Errors = append(errs.Errors, fmt.Errorf("port should greater than 0"))
	}
	if s.Recurse.TimeoutSec <= 0 {
		errs.Errors = append(errs.Errors, fmt.Errorf("recurse.timeout should greater than 0"))
	}
	if len(s.Resolvers) == 0 {
		errs.Errors = append(errs.Errors, fmt.Errorf("you should at least config one resolver"))
	}
	for idx, resolverConfig := range s.Resolvers {
		if len(resolverConfig.Name) == 0 {
			errs.Errors = append(errs.Errors, fmt.Errorf("resolver %d config name is empty", idx))
		}
		if resolverConfig.DnsTtl < 0 {
			errs.Errors = append(errs.Errors, fmt.Errorf("resolver %d config dnsttl should greater or equals to 0",
				idx))
		}
		if resolverConfig.Enable {
			if resolverConfig.Name == common.PluginNameDnsAgent {
				s.DnsEnabled = true
			} else if resolverConfig.Name == common.PluginNameMeshProxy {
				s.MeshEnabled = true
			}
		}
	}
	if !s.DnsEnabled && !s.MeshEnabled {
		errs.Errors = append(errs.Errors, fmt.Errorf("you should at least enable one resolver"))
	}
	if err := errs.ErrorOrNil(); err != nil {
		log.Errorf("[config] sidecar config verify failed: %v", err)
		return err
	}
	return nil
}
