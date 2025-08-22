package constants

const (
	EnvPolarisAddress          = "POLARIS_ADDRESS"
	EnvSidecarBind             = "SIDECAR_BIND"
	EnvSidecarPort             = "SIDECAR_PORT"
	EnvSidecarNamespace        = "SIDECAR_NAMESPACE"
	EnvSidecarRegion           = "SIDECAR_REGION"
	EnvSidecarZone             = "SIDECAR_ZONE"
	EnvSidecarCampus           = "SIDECAR_CAMPUS"
	EnvSidecarNearbyMatchLevel = "SIDECAR_NEARBY_MATCH_LEVEL"
	// recurse env
	EnvSidecarRecurseEnable  = "SIDECAR_RECURSE_ENABLE"
	EnvSidecarRecurseTimeout = "SIDECAR_RECURSE_TIMEOUT"
	// log env
	EnvSidecarLogRotateOutputPath      = "SIDECAR_LOG_ROTATE_OUTPUT_PATH"
	EnvSidecarLogErrorRotateOutputPath = "SIDECAR_LOG_ERROR_ROTATE_OUTPUT_PATH"
	EnvSidecarLogRotationMaxSize       = "SIDECAR_LOG_ROTATION_MAX_SIZE"
	EnvSidecarLogRotationMaxBackups    = "SIDECAR_LOG_ROTATION_MAX_BACKUPS"
	EnvSidecarLogRotationMaxAge        = "SIDECAR_LOG_ROTATION_MAX_AGE"
	EnvSidecarLogLevel                 = "SIDECAR_LOG_LEVEL"
	// dns env
	EnvSidecarDnsTtl         = "SIDECAR_DNS_TTL"
	EnvSidecarDnsEnable      = "SIDECAR_DNS_ENABLE"
	EnvSidecarDnsSuffix      = "SIDECAR_DNS_SUFFIX"
	EnvSidecarDnsRouteLabels = "SIDECAR_DNS_ROUTE_LABELS"
	// mesh env
	EnvSidecarMeshTtl            = "SIDECAR_MESH_TTL"
	EnvSidecarMeshEnable         = "SIDECAR_MESH_ENABLE"
	EnvSidecarMeshReloadInterval = "SIDECAR_MESH_RELOAD_INTERVAL"
	EnvSidecarMeshAnswerIp       = "SIDECAR_MESH_ANSWER_IP"
	EnvSidecarMtlsEnable         = "SIDECAR_MTLS_ENABLE"
	EnvSidecarMtlsCAServer       = "SIDECAR_MTLS_CA_SERVER"
	EnvSidecarRLSEnable          = "SIDECAR_RLS_ENABLE"
	EnvSidecarMetricEnable       = "SIDECAR_METRIC_ENABLE"
	EnvSidecarMetricListenPort   = "SIDECAR_METRIC_LISTEN_PORT"
)
