package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	"neo-code/internal/feishuadapter"
	"neo-code/internal/gateway/transport"
)

var runFeishuAdapterCommand = defaultFeishuAdapterCommandRunner
var newGatewayRPCClientForFeishu = feishuadapter.NewGatewayRPCClient
var newFeishuMessenger = feishuadapter.NewFeishuMessenger
var newFeishuAdapter = feishuadapter.New

type feishuAdapterCommandOptions struct {
	Listen                 string
	EventPath              string
	CardPath               string
	AppID                  string
	AppSecret              string
	VerifyToken            string
	SigningSecret          string
	InsecureSkipSignVerify bool
	IdempotencyTTLSec      int
	RequestTimeoutSec      int
	ReconnectBackoffMinM   int
	ReconnectBackoffMaxM   int
	RebindIntervalSec      int
	GatewayListen          string
	GatewayTokenFile       string
}

// newFeishuAdapterCommand 创建飞书适配器子命令，用于桥接飞书事件与网关运行链路。
func newFeishuAdapterCommand() *cobra.Command {
	options := &feishuAdapterCommandOptions{}
	cmd := &cobra.Command{
		Use:          "feishu-adapter",
		Short:        "Start Feishu adapter bridge for gateway",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			commandAnnotationSkipGlobalPreload:     "true",
			commandAnnotationSkipSilentUpdateCheck: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeishuAdapterCommand(cmd.Context(), *options)
		},
	}

	cmd.Flags().StringVar(&options.Listen, "listen", "", "feishu adapter listen address (e.g. 127.0.0.1:18080)")
	cmd.Flags().StringVar(&options.EventPath, "event-path", "", "feishu event callback path")
	cmd.Flags().StringVar(&options.CardPath, "card-path", "", "feishu card callback path")
	cmd.Flags().StringVar(&options.AppID, "app-id", "", "feishu app id")
	cmd.Flags().StringVar(&options.AppSecret, "app-secret", "", "feishu app secret")
	cmd.Flags().StringVar(&options.VerifyToken, "verify-token", "", "feishu verify token")
	cmd.Flags().StringVar(&options.SigningSecret, "signing-secret", "", "feishu signing secret")
	cmd.Flags().BoolVar(&options.InsecureSkipSignVerify, "insecure-skip-signature-verify", false, "skip feishu callback signature verification (unsafe)")
	cmd.Flags().IntVar(&options.IdempotencyTTLSec, "idempotency-ttl-sec", 0, "idempotency ttl in seconds")
	cmd.Flags().IntVar(&options.RequestTimeoutSec, "request-timeout-sec", 0, "gateway request timeout in seconds")
	cmd.Flags().IntVar(&options.ReconnectBackoffMinM, "reconnect-backoff-min-ms", 0, "gateway reconnect min backoff in ms")
	cmd.Flags().IntVar(&options.ReconnectBackoffMaxM, "reconnect-backoff-max-ms", 0, "gateway reconnect max backoff in ms")
	cmd.Flags().IntVar(&options.RebindIntervalSec, "rebind-interval-sec", 0, "gateway active-session rebind interval in sec")
	cmd.Flags().StringVar(&options.GatewayListen, "gateway-listen", "", "gateway listen address override")
	cmd.Flags().StringVar(&options.GatewayTokenFile, "gateway-token-file", "", "gateway auth token file override")

	return cmd
}

// defaultFeishuAdapterCommandRunner 读取配置并启动飞书适配器主循环。
func defaultFeishuAdapterCommandRunner(ctx context.Context, options feishuAdapterCommandOptions) error {
	loader := config.NewLoader("", config.StaticDefaults())
	cfg, err := loader.Load(ctx)
	if err != nil {
		return err
	}
	merged := mergeFeishuOptions(cfg.Feishu, options, cfg.Gateway)
	if err := merged.Validate(); err != nil {
		return err
	}
	logger := log.New(os.Stderr, "neocode-feishu-adapter: ", log.LstdFlags)

	gatewayClient, err := newGatewayRPCClientForFeishu(feishuadapter.GatewayClientConfig{
		ListenAddress:  merged.GatewayListenAddress,
		TokenFile:      merged.GatewayTokenFile,
		RequestTimeout: time.Duration(merged.RequestTimeoutSec) * time.Second,
	})
	if err != nil {
		return fmt.Errorf("init gateway client: %w", err)
	}
	messenger := newFeishuMessenger(merged.AppID, merged.AppSecret, nil)
	adapter, err := newFeishuAdapter(merged.ToAdapterConfig(), gatewayClient, messenger, logger)
	if err != nil {
		_ = gatewayClient.Close()
		return err
	}
	return adapter.Run(ctx)
}

type mergedFeishuOptions struct {
	Enabled                bool
	Listen                 string
	EventPath              string
	CardPath               string
	AppID                  string
	AppSecret              string
	VerifyToken            string
	SigningSecret          string
	InsecureSkipSignVerify bool
	IdempotencyTTLSec      int
	RequestTimeoutSec      int
	ReconnectBackoffMinMs  int
	ReconnectBackoffMaxMs  int
	RebindIntervalSec      int
	GatewayListenAddress   string
	GatewayTokenFile       string
}

// Validate 校验合并后的飞书参数，确保适配器启动前失败前置。
func (o mergedFeishuOptions) Validate() error {
	cfg := o.ToAdapterConfig()
	return cfg.Validate()
}

// ToAdapterConfig 将 CLI/配置合并结果转换为适配器运行配置。
func (o mergedFeishuOptions) ToAdapterConfig() feishuadapter.Config {
	return feishuadapter.Config{
		ListenAddress:          o.Listen,
		EventPath:              o.EventPath,
		CardPath:               o.CardPath,
		AppID:                  o.AppID,
		AppSecret:              o.AppSecret,
		VerifyToken:            o.VerifyToken,
		SigningSecret:          o.SigningSecret,
		InsecureSkipSignVerify: o.InsecureSkipSignVerify,
		RequestTimeout:         time.Duration(o.RequestTimeoutSec) * time.Second,
		IdempotencyTTL:         time.Duration(o.IdempotencyTTLSec) * time.Second,
		ReconnectBackoffMin:    time.Duration(o.ReconnectBackoffMinMs) * time.Millisecond,
		ReconnectBackoffMax:    time.Duration(o.ReconnectBackoffMaxMs) * time.Millisecond,
		RebindInterval:         time.Duration(o.RebindIntervalSec) * time.Second,
	}
}

// mergeFeishuOptions 合并 config.yaml 与命令行参数，命令行优先。
func mergeFeishuOptions(feishuCfg config.FeishuConfig, cliOptions feishuAdapterCommandOptions, gatewayCfg config.GatewayConfig) mergedFeishuOptions {
	feishuCfg.ApplyDefaults(config.FeishuConfig{
		Adapter: config.FeishuAdapterConfig{
			Listen:   config.DefaultFeishuAdapterListen,
			EventURI: config.DefaultFeishuAdapterEventPath,
			CardURI:  config.DefaultFeishuAdapterCardPath,
		},
		RequestTimeoutSec:    config.DefaultFeishuGatewayRequestTimeoutSec,
		IdempotencyTTLSec:    config.DefaultFeishuIdempotencyTTLSec,
		ReconnectBackoffMinM: config.DefaultFeishuReconnectBackoffMinMs,
		ReconnectBackoffMaxM: config.DefaultFeishuReconnectBackoffMaxMs,
		RebindIntervalSec:    config.DefaultFeishuRebindIntervalSec,
	})
	merged := mergedFeishuOptions{
		Enabled:                feishuCfg.Enabled,
		Listen:                 strings.TrimSpace(feishuCfg.Adapter.Listen),
		EventPath:              strings.TrimSpace(feishuCfg.Adapter.EventURI),
		CardPath:               strings.TrimSpace(feishuCfg.Adapter.CardURI),
		AppID:                  strings.TrimSpace(feishuCfg.AppID),
		AppSecret:              strings.TrimSpace(feishuCfg.AppSecret),
		VerifyToken:            strings.TrimSpace(feishuCfg.VerifyToken),
		SigningSecret:          strings.TrimSpace(feishuCfg.SigningSecret),
		InsecureSkipSignVerify: feishuCfg.InsecureSkipSignVerify,
		IdempotencyTTLSec:      feishuCfg.IdempotencyTTLSec,
		RequestTimeoutSec:      feishuCfg.RequestTimeoutSec,
		ReconnectBackoffMinMs:  feishuCfg.ReconnectBackoffMinM,
		ReconnectBackoffMaxMs:  feishuCfg.ReconnectBackoffMaxM,
		RebindIntervalSec:      feishuCfg.RebindIntervalSec,
		GatewayListenAddress:   strings.TrimSpace(feishuCfg.GatewayClient.ListenAddress),
		GatewayTokenFile:       strings.TrimSpace(feishuCfg.GatewayClient.TokenFile),
	}
	if merged.GatewayListenAddress == "" {
		if address, err := transport.DefaultListenAddress(); err == nil {
			merged.GatewayListenAddress = address
		}
	}
	if merged.GatewayTokenFile == "" {
		merged.GatewayTokenFile = strings.TrimSpace(gatewayCfg.Security.TokenFile)
	}

	if value := strings.TrimSpace(cliOptions.Listen); value != "" {
		merged.Listen = value
	}
	if value := strings.TrimSpace(cliOptions.EventPath); value != "" {
		merged.EventPath = value
	}
	if value := strings.TrimSpace(cliOptions.CardPath); value != "" {
		merged.CardPath = value
	}
	if value := strings.TrimSpace(cliOptions.AppID); value != "" {
		merged.AppID = value
	}
	if value := strings.TrimSpace(cliOptions.AppSecret); value != "" {
		merged.AppSecret = value
	}
	if value := strings.TrimSpace(cliOptions.VerifyToken); value != "" {
		merged.VerifyToken = value
	}
	if value := strings.TrimSpace(cliOptions.SigningSecret); value != "" {
		merged.SigningSecret = value
	}
	if cliOptions.InsecureSkipSignVerify {
		merged.InsecureSkipSignVerify = true
	}
	if cliOptions.IdempotencyTTLSec > 0 {
		merged.IdempotencyTTLSec = cliOptions.IdempotencyTTLSec
	}
	if cliOptions.RequestTimeoutSec > 0 {
		merged.RequestTimeoutSec = cliOptions.RequestTimeoutSec
	}
	if cliOptions.ReconnectBackoffMinM > 0 {
		merged.ReconnectBackoffMinMs = cliOptions.ReconnectBackoffMinM
	}
	if cliOptions.ReconnectBackoffMaxM > 0 {
		merged.ReconnectBackoffMaxMs = cliOptions.ReconnectBackoffMaxM
	}
	if cliOptions.RebindIntervalSec > 0 {
		merged.RebindIntervalSec = cliOptions.RebindIntervalSec
	}
	if value := strings.TrimSpace(cliOptions.GatewayListen); value != "" {
		merged.GatewayListenAddress = value
	}
	if value := strings.TrimSpace(cliOptions.GatewayTokenFile); value != "" {
		merged.GatewayTokenFile = value
	}
	return merged
}
