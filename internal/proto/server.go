package proto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	"github.com/rancher-sandbox/cluster-api-provider-elemental/api/v1beta1"
	"github.com/rancher-sandbox/cluster-api-provider-elemental/internal/log"
	pb "github.com/rancher-sandbox/cluster-api-provider-elemental/pkg/api/proto/v1"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
)

const (
	channelBufferSize = 2
)

var (
	gracefulShutdownTimeout = 30 * time.Second
)

type Server interface {
	Start(ctx context.Context, network, address, certFile, keyFile string) error
	SendHostUpdate(key types.NamespacedName)
}

type server struct {
	pb.UnimplementedElementalServer
	logger    logr.Logger
	k8sClient client.Client
	hosts     map[string](chan struct{})
}

func NewServer(logger logr.Logger, client client.Client) Server {
	return &server{
		logger:    logger,
		k8sClient: client,
		hosts:     make(map[string](chan struct{})),
	}
}

func (s *server) SendHostUpdate(key types.NamespacedName) {
	if hostChan, found := s.hosts[key.String()]; found {
		logger := s.logger.WithValues(log.KeyNamespace, key.Namespace).
			WithValues(log.KeyElementalHost, key.Name)
		select {
		case hostChan <- struct{}{}:
			logger.WithCallDepth(log.DebugLevel).Info("Enqueuing update through gRPC")
		default:
			logger.Info("Could not enqueue ElementalHost update. Buffer is full.")
		}
	}
}

func (s *server) Start(ctx context.Context, network, address, certFile, keyFile string) error {
	s.logger.Info("Initializing gRPC server")

	listener, err := net.Listen(network, address)
	if err != nil {
		return fmt.Errorf("listening on %s %s: %w", network, address, err)
	}
	var opts []grpc.ServerOption

	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("reading TLS credentials: %w", err)
	}
	opts = []grpc.ServerOption{grpc.Creds(creds)}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterElementalServer(grpcServer, s)

	select {
	case <-ctx.Done():
		done := make(chan struct{})
		go func() {
			s.logger.Info("Gracefully shutting down gRPC server")
			grpcServer.GracefulStop()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(gracefulShutdownTimeout):
			s.logger.Info("Forcefully shutting down gRPC server")
			grpcServer.Stop()
		}
	default:
		s.logger.Info("Starting gRPC server")
		if err := grpcServer.Serve(listener); err != nil {
			return fmt.Errorf("serving grpc: %w", err)
		}
	}
	return nil
}

func (s *server) GetRegistration(context.Context, *pb.RegistrationRequest) (*pb.RegistrationResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetRegistration not implemented")
}
func (s *server) CreateHost(context.Context, *pb.HostCreateRequest) (*pb.HostResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateHost not implemented")
}
func (s *server) DeleteHost(context.Context, *pb.HostDeleteRequest) (*pb.HostDeleteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteHost not implemented")
}
func (s *server) GetBootstrap(context.Context, *pb.BootstrapRequest) (*pb.BootstrapResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetBootstrap not implemented")
}
func (s *server) ReconcileHost(stream grpc.BidiStreamingServer[pb.HostPatchRequest, pb.HostResponse]) error {
	incoming, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		s.logger.Info("Stream closed before any message was received")
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading first time stream: %w", err)
	}
	// !! Validation/Auth can happen here !!
	logger := s.logger.WithValues(log.KeyNamespace, incoming.Namespace).
		WithValues(log.KeyElementalHost, incoming.Name)

	// Since we no longer validate messages after the stream is open, it's important to set a static host key.
	// This is to prevent the host from assuming different identities (patching other hosts) after authentication.
	hostKey := client.ObjectKey{Namespace: incoming.Namespace, Name: incoming.Name}

	// Always send back a first response.
	// This gives the consumer a chance to reconcile from previously unreceived messages,
	// even if the ElementalHost has not mutated meanwhile.
	if err := s.sendElementalHostToStream(hostKey, stream); err != nil {
		return fmt.Errorf("sending first ElementalHost to stream: %w", err)
	}

	// Asynchronously patch ElementalHost resource from stream input
	// Note: stream.Recv() can be consumed concurrently to stream.Send()
	go func() {
		if err := s.updateElementalHostFromStream(logger, hostKey, stream); err != nil {
			logger.Error(err, "Failed to consume stream")
			return
		}
	}()

	// Send ElementalHost updates
	s.hosts[hostKey.String()] = make(chan struct{}, channelBufferSize)
	defer delete(s.hosts, hostKey.String())

	for {
		select {
		case <-s.hosts[hostKey.String()]:
			logger.Info("Sending update")
			if err := s.sendElementalHostToStream(hostKey, stream); err != nil {
				return fmt.Errorf("sending ElementalHost to stream: %w", err)
			}
		case <-stream.Context().Done():
			// Stream is closed
			return nil
		}
	}
}

func (s *server) sendElementalHostToStream(key types.NamespacedName, stream grpc.BidiStreamingServer[pb.HostPatchRequest, pb.HostResponse]) error {
	elementalHost := &v1beta1.ElementalHost{}
	if err := s.k8sClient.Get(stream.Context(), key, elementalHost); err != nil {
		return fmt.Errorf("getting ElementalHost: %w", err)
	}

	response, err := getElementalHostResponse(*elementalHost)
	if err != nil {
		return fmt.Errorf("getting HostResponse: %w", err)
	}

	if err := stream.Send(response); err != nil {
		return fmt.Errorf("sending HostResponse: %w", err)
	}
	return nil
}

func (s *server) updateElementalHostFromStream(logger logr.Logger, key types.NamespacedName, stream grpc.BidiStreamingServer[pb.HostPatchRequest, pb.HostResponse]) error {
	for {
		incoming, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			logger.Info("Stream closed")
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading stream: %w", err)
		}
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Always refresh resource on each attempt
			elementalHost := &v1beta1.ElementalHost{}
			if err := s.k8sClient.Get(stream.Context(), key, elementalHost); err != nil {
				return fmt.Errorf("getting ElementalHost: %w", err)
			}

			// Patch the object
			patchHelper, err := patch.NewHelper(elementalHost, s.k8sClient)
			if err != nil {
				return fmt.Errorf("initializing patch helper: %w", err)
			}

			applyPatchRequestToElementalHost(incoming, elementalHost)

			return patchHelper.Patch(stream.Context(), elementalHost)
		})
		if err != nil {
			return fmt.Errorf("patching ElementalHost: %w", err)
		}
	}
}

func applyPatchRequestToElementalHost(patch *pb.HostPatchRequest, elementalHost *v1beta1.ElementalHost) {
	if elementalHost.Annotations == nil {
		elementalHost.Annotations = map[string]string{}
	}
	if elementalHost.Labels == nil {
		elementalHost.Labels = map[string]string{}
	}
	maps.Copy(elementalHost.Annotations, patch.Annotations)
	maps.Copy(elementalHost.Labels, patch.Labels)
	// Map request values to ElementalHost labels
	if patch.Installed {
		elementalHost.Labels[v1beta1.LabelElementalHostInstalled] = "true"
	}
	if patch.Bootstrapped {
		elementalHost.Labels[v1beta1.LabelElementalHostBootstrapped] = "true"
	}
	if patch.Reset_ {
		elementalHost.Labels[v1beta1.LabelElementalHostReset] = "true"
	}
	switch patch.InPlaceUpdate {
	case pb.InPlaceUpdate_IN_PLACE_UPDATE_DONE:
		elementalHost.Labels[v1beta1.LabelElementalHostInPlaceUpdate] = v1beta1.InPlaceUpdateDone
	case pb.InPlaceUpdate_IN_PLACE_UPDATE_PENDING:
		elementalHost.Labels[v1beta1.LabelElementalHostInPlaceUpdate] = v1beta1.InPlaceUpdatePending
	case pb.InPlaceUpdate_IN_PLACE_UPDATE_UNSPECIFIED:
		// Do nothing. Users are expected to remove the label manually after confirming the "Done" value.
	}
	if elementalHost.Status.Conditions == nil {
		elementalHost.Status.Conditions = clusterv1.Conditions{}
	}
	// Set the patch condition to the ElementalHost object.
	conditions.Set(elementalHost, &clusterv1.Condition{
		Type:     clusterv1.ConditionType(patch.Condition.Type),
		Status:   corev1.ConditionStatus(patch.Condition.Status),
		Severity: clusterv1.ConditionSeverity(patch.Condition.Severity),
		Reason:   patch.Condition.Reason,
		Message:  patch.Condition.Message,
	})
	// Always update the Summary after conditions change
	conditions.SetSummary(elementalHost)

	switch patch.Phase {
	case pb.HostPhase_HOST_PHASE_UNSPECIFIED:
		elementalHost.Status.Phase = v1beta1.PhaseUnknown
	case pb.HostPhase_HOST_PHASE_REGISTERING:
		elementalHost.Status.Phase = v1beta1.PhaseRegistering
	case pb.HostPhase_HOST_PHASE_FINALIZING_REGISTRATION:
		elementalHost.Status.Phase = v1beta1.PhaseFinalizingRegistration
	case pb.HostPhase_HOST_PHASE_INSTALLING:
		elementalHost.Status.Phase = v1beta1.PhaseInstalling
	case pb.HostPhase_HOST_PHASE_BOOTSTRAPPING:
		elementalHost.Status.Phase = v1beta1.PhaseBootstrapping
	case pb.HostPhase_HOST_PHASE_RUNNING:
		elementalHost.Status.Phase = v1beta1.PhaseRunning
	case pb.HostPhase_HOST_PHASE_TRIGGERING_RESET:
		elementalHost.Status.Phase = v1beta1.PhaseTriggeringReset
	case pb.HostPhase_HOST_PHASE_RESETTING:
		elementalHost.Status.Phase = v1beta1.PhaseResetting
	case pb.HostPhase_HOST_PHASE_RECONCILING_OS_VERSION:
		elementalHost.Status.Phase = v1beta1.PhaseOSVersionReconcile
	}
}

func getElementalHostResponse(elementalHost v1beta1.ElementalHost) (*pb.HostResponse, error) {
	response := &pb.HostResponse{}
	response.Name = elementalHost.Name
	response.Annotations = elementalHost.Annotations
	response.Labels = elementalHost.Labels
	response.BootstrapReady = elementalHost.Spec.BootstrapSecret != nil

	// Map labels
	if elementalHost.Labels != nil {
		if value, found := elementalHost.Labels[v1beta1.LabelElementalHostBootstrapped]; found && value == "true" {
			response.Boostrapped = true
		}
		if value, found := elementalHost.Labels[v1beta1.LabelElementalHostInstalled]; found && value == "true" {
			response.Installed = true
		}
		if value, found := elementalHost.Labels[v1beta1.LabelElementalHostNeedsReset]; found && value == "true" {
			response.NeedsReset = true
		}
		if value, found := elementalHost.Labels[v1beta1.LabelElementalHostInPlaceUpdate]; found {
			switch value {
			case v1beta1.InPlaceUpdatePending:
				response.InPlaceUpdate = pb.InPlaceUpdate_IN_PLACE_UPDATE_PENDING
			case v1beta1.InPlaceUpdateDone:
				response.InPlaceUpdate = pb.InPlaceUpdate_IN_PLACE_UPDATE_DONE
			}
		}
	}

	// Convert OSVersionManagement object to JSON bytes
	osVersionManagementBytes, err := json.Marshal(elementalHost.Spec.OSVersionManagement)
	if err != nil {
		return nil, fmt.Errorf("marshalling OSVersionManagement: %w", err)
	}
	response.OsVersionManagement = osVersionManagementBytes
	return response, nil
}
