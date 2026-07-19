// Package artifactcontrol 装配本地产品共用的 ProducedResource/Artifact 控制面。
package artifactcontrol

import (
	"fmt"
	"path/filepath"
	"strings"

	artifactcontract "genesis-agent/internal/capabilities/artifact/contract"
	artifactmodel "genesis-agent/internal/capabilities/artifact/model"
	artifactservice "genesis-agent/internal/capabilities/artifact/service"
	selectartifact "genesis-agent/internal/capabilities/artifact/tool/select_deliverable_candidate"
	sandboxcontract "genesis-agent/internal/capabilities/sandbox/contract"
	scriptservice "genesis-agent/internal/capabilities/skill/script/service"
	toolcontract "genesis-agent/internal/capabilities/tool/contract"
	workspaceadapter "genesis-agent/internal/capabilities/workspace/adapter/sandbox"
	workcontract "genesis-agent/internal/capabilities/workspace/contract"
	execmodel "genesis-agent/internal/capabilities/execution/model"
	workmodel "genesis-agent/internal/capabilities/workspace/model"
	workservice "genesis-agent/internal/capabilities/workspace/service"
	"genesis-agent/internal/platform/idgen"
	localartifact "genesis-agent/shared/local/artifact"
	localworkspace "genesis-agent/shared/local/workspace"
)

const defaultBufferedObjectMaxBytes = int64(512 * 1024 * 1024)

// Options 描述本地产品 Artifact 控制面装配参数。
type Options struct {
	StateRoot             string
	DeliveryWorkspaceRoot string
	FileClient            sandboxcontract.FileSystemClient
	// SandboxRenewer 可选。若实现 SessionRenewer，则用于发布前/读取时对 session-file 续租。
	// 未设置时若 FileClient 实现 SessionRenewer 则自动复用。仅有 RenewSandbox 不足以装配 session-file 续租。
	SandboxRenewer workspaceadapter.SandboxRenewer
	// ArtifactDownloader 可选。genesis-sandbox 正式能力应为流式 OpenObject；
	// DownloadArtifact 的 []byte 缓冲适配仅开发期可用，生产完成态需跨仓替换（见架构文档 §10.1 / §22.1）。
	ArtifactDownloader     workspaceadapter.ArtifactByteDownloader
	BufferedObjectMaxBytes int64
}

// Control 是 CLI/Desktop 等产品复用的 Artifact 控制面组件集合。
type Control struct {
	Produced       workcontract.ProducedResourceRegistrar
	RemoteSessions scriptservice.RemoteSessionBinder
	Reservations   artifactcontract.OutputReservationAllocator
	Deliverables   artifactcontract.DeliverableSpecStore
	Finalizer      artifactcontract.RequiredDeliverableFinalizer
	Initializer    artifactcontract.RunInitializer
	Completion     artifactcontract.CompletionPolicy
	QAEvidence     artifactcontract.QAEvidenceRecorder
	Selector       toolcontract.Tool
}

// Build 装配完整 ProducedResource/Artifact 控制面。
func Build(opts Options) (Control, error) {
	if strings.TrimSpace(opts.StateRoot) == "" {
		return Control{}, fmt.Errorf("artifact control state root 无效")
	}
	maxBytes := opts.BufferedObjectMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultBufferedObjectMaxBytes
	}
	ids := idgen.NewUUIDGenerator()
	manifests, err := localworkspace.NewManifestStore(opts.StateRoot)
	if err != nil {
		return Control{}, err
	}
	producedStore, err := localworkspace.NewProducedResourceStore(opts.StateRoot)
	if err != nil {
		return Control{}, err
	}
	hostLocators, err := localworkspace.NewHostLocatorStore(opts.StateRoot)
	if err != nil {
		return Control{}, err
	}
	hostResolver, err := localworkspace.NewHostBackendResourceResolver(hostLocators, ids)
	if err != nil {
		return Control{}, err
	}
	hostReader, err := localworkspace.NewHostResourceReader(hostLocators)
	if err != nil {
		return Control{}, err
	}

	resolverRoutes := []workcontract.BackendResourceResolverRoute{
		{Backend: execmodel.BackendKindHost, Availability: workmodel.ResourceAvailabilityDurable, Resolver: hostResolver},
		{Backend: execmodel.BackendKindLocalSandbox, Availability: workmodel.ResourceAvailabilityDurable, Resolver: hostResolver},
	}
	readerRoutes := []workcontract.ResourceReaderRoute{{Authority: "host", Scheme: "run-file", Reader: hostReader}}
	remoteRoot := filepath.Join(opts.StateRoot, "cache", "remote-resources")
	remoteSessions, err := workspaceadapter.NewFileSessionBindingStore(filepath.Join(remoteRoot, "sessions"))
	if err != nil {
		return Control{}, err
	}
	var leaseKeeper artifactservice.LeaseKeeper
	if opts.FileClient != nil {
		remoteLocators, err := workspaceadapter.NewFileRemoteLocatorStore(filepath.Join(remoteRoot, "locators"))
		if err != nil {
			return Control{}, err
		}
		remoteResolver, err := workspaceadapter.NewSessionFileResolver(opts.FileClient, remoteSessions, remoteLocators, ids)
		if err != nil {
			return Control{}, err
		}
		remoteReader, err := workspaceadapter.NewSessionFileReader(opts.FileClient, remoteLocators)
		if err != nil {
			return Control{}, err
		}
		var sessionRenewer workspaceadapter.SessionRenewer
		if candidate, ok := opts.SandboxRenewer.(workspaceadapter.SessionRenewer); ok {
			sessionRenewer = candidate
		} else if candidate, ok := opts.FileClient.(workspaceadapter.SessionRenewer); ok {
			sessionRenewer = candidate
		}
		if sessionRenewer != nil {
			remoteReader = remoteReader.WithRenewer(sessionRenewer)
			keeper, err := workspaceadapter.NewSessionLeaseKeeper(remoteLocators, sessionRenewer)
			if err != nil {
				return Control{}, err
			}
			leaseKeeper = keeper
		}
		resolverRoutes = append(resolverRoutes, workcontract.BackendResourceResolverRoute{
			Backend: execmodel.BackendKindRemote, Availability: workmodel.ResourceAvailabilityLeased, Resolver: remoteResolver,
		})
		readerRoutes = append(readerRoutes, workcontract.ResourceReaderRoute{
			Authority: workspaceadapter.RemoteExecutorAuthority, Scheme: workspaceadapter.SessionFileScheme, Reader: remoteReader,
		})
		executorResolver, err := workspaceadapter.NewExecutorObjectResolver(remoteLocators, ids)
		if err != nil {
			return Control{}, err
		}
		resolverRoutes = append(resolverRoutes, workcontract.BackendResourceResolverRoute{
			Backend: execmodel.BackendKindRemote, Availability: workmodel.ResourceAvailabilityDurable, Resolver: executorResolver,
		})
		var objectClient workspaceadapter.ExecutorObjectClient
		if opts.ArtifactDownloader != nil {
			// genesis-sandbox 正式能力应为流式 OpenObject；DownloadArtifact 仅作开发期临时适配，禁止复制到宿主临时目录冒充 durable。
			buffered, err := workspaceadapter.NewBufferedExecutorObjectClient(opts.ArtifactDownloader, maxBytes)
			if err != nil {
				return Control{}, err
			}
			objectClient = buffered
		}
		if objectClient != nil {
			executorReader, err := workspaceadapter.NewExecutorObjectReader(objectClient, remoteLocators)
			if err != nil {
				return Control{}, err
			}
			readerRoutes = append(readerRoutes, workcontract.ResourceReaderRoute{
				Authority: workspaceadapter.RemoteExecutorAuthority, Scheme: workspaceadapter.ExecutorObjectScheme, Reader: executorReader,
			})
		}
	}
	resolverRouter, err := workservice.NewBackendResourceResolverRouter(resolverRoutes)
	if err != nil {
		return Control{}, err
	}
	readerRouter, err := workservice.NewResourceReaderRouter(readerRoutes)
	if err != nil {
		return Control{}, err
	}
	registrar, err := workservice.NewProducedResourceRegistrar(manifests, producedStore, resolverRouter, ids)
	if err != nil {
		return Control{}, err
	}

	ledger, err := localartifact.NewLedgerStore(opts.StateRoot)
	if err != nil {
		return Control{}, err
	}
	store, err := localartifact.NewStore(opts.StateRoot)
	if err != nil {
		return Control{}, err
	}
	publisher, err := artifactservice.NewArtifactPublicationService(ledger, ledger, ledger, producedStore, manifests, readerRouter, store, artifactservice.BasicGate{})
	if err != nil {
		return Control{}, err
	}
	publisher = publisher.WithLeaseKeeper(leaseKeeper)
	deliveryRoot := opts.DeliveryWorkspaceRoot
	if strings.TrimSpace(deliveryRoot) == "" {
		deliveryRoot = opts.StateRoot
	}
	targets, err := localartifact.NewTargetRegistry(map[string]string{"workspace-root": deliveryRoot})
	if err != nil {
		return Control{}, err
	}
	materializer, err := localartifact.NewMaterializer(store, targets)
	if err != nil {
		return Control{}, err
	}
	// "run-output" 是历史策略名：表示按产品默认交付，不是写入 runtime/runs/.../output。
	// CLI 将其映射为项目根（DeliveryProjectRoot）；用户可见文件不落在 Run 内部 output 目录。
	planner, err := localartifact.NewPolicyTargetPlanner(map[string]artifactmodel.DeliveryTarget{
		"run-output": {Kind: artifactmodel.DeliveryProjectRoot, Resource: workmodel.ResourceRef{Authority: "host", Scheme: "delivery-root", ID: "workspace-root"}, Name: "$artifact_name"},
	})
	if err != nil {
		return Control{}, err
	}
	delivery, err := artifactservice.NewDeliveryService(ledger, ledger, ledger, ledger, store, planner, materializer)
	if err != nil {
		return Control{}, err
	}
	finalizer, err := artifactservice.NewDeterministicFinalizer(ledger, ledger, producedStore, publisher, delivery)
	if err != nil {
		return Control{}, err
	}
	initializer, err := artifactservice.NewTaskDeliverableInitializer(ledger)
	if err != nil {
		return Control{}, err
	}
	completion, err := artifactservice.NewCompletionEvaluator(ledger, ledger, ledger, ledger, ledger)
	if err != nil {
		return Control{}, err
	}
	qaEvidence, err := artifactservice.NewQAEvidenceRecorder(ledger, ledger, ledger, ledger)
	if err != nil {
		return Control{}, err
	}
	reservations, err := artifactservice.NewOutputReservationService(ledger, ids)
	if err != nil {
		return Control{}, err
	}
	selector, err := selectartifact.New(finalizer)
	if err != nil {
		return Control{}, err
	}
	return Control{
		Produced:       registrar,
		RemoteSessions: remoteSessions,
		Reservations:   reservations,
		Deliverables:   ledger,
		Finalizer:      finalizer,
		Initializer:    initializer,
		Completion:     completion,
		QAEvidence:     qaEvidence,
		Selector:       selector,
	}, nil
}
