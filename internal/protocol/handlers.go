package protocol

import (
	"fmt"

	"github.com/deeklead/horde/internal/drums"
)

// Handler processes a protocol message and returns an error if processing failed.
type Handler func(msg *drums.Message) error

// HandlerRegistry maps message types to their handlers.
type HandlerRegistry struct {
	handlers map[MessageType]Handler
}

// NewHandlerRegistry creates a new handler registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[MessageType]Handler),
	}
}

// Register adds a handler for a specific message type.
func (r *HandlerRegistry) Register(msgType MessageType, handler Handler) {
	r.handlers[msgType] = handler
}

// Handle dispatches a message to the appropriate handler.
// Returns an error if no handler is registered for the message type.
func (r *HandlerRegistry) Handle(msg *drums.Message) error {
	msgType := ParseMessageType(msg.Subject)
	if msgType == "" {
		return fmt.Errorf("unknown message type for subject: %s", msg.Subject)
	}

	handler, ok := r.handlers[msgType]
	if !ok {
		return fmt.Errorf("no handler registered for message type: %s", msgType)
	}

	return handler(msg)
}

// CanHandle returns true if a handler is registered for the message's type.
func (r *HandlerRegistry) CanHandle(msg *drums.Message) bool {
	msgType := ParseMessageType(msg.Subject)
	if msgType == "" {
		return false
	}

	_, ok := r.handlers[msgType]
	return ok
}

// WitnessHandler defines the interface for Witness protocol handlers.
// The Witness receives messages from Forge about merge status.
type WitnessHandler interface {
	// HandleMerged is called when a branch was successfully merged.
	HandleMerged(payload *MergedPayload) error

	// HandleMergeFailed is called when a merge attempt failed.
	HandleMergeFailed(payload *MergeFailedPayload) error

	// HandleReworkRequest is called when a branch needs rebasing.
	HandleReworkRequest(payload *ReworkRequestPayload) error
}

// ForgeHandler defines the interface for Forge protocol handlers.
// The Forge receives messages from Witness about ready branches.
type ForgeHandler interface {
	// HandleMergeReady is called when a raider's work is verified and ready.
	HandleMergeReady(payload *MergeReadyPayload) error
}

// WrapWitnessHandlers creates drums handlers from a WitnessHandler.
func WrapWitnessHandlers(h WitnessHandler) *HandlerRegistry {
	registry := NewHandlerRegistry()

	registry.Register(TypeMerged, func(msg *drums.Message) error {
		payload := ParseMergedPayload(msg.Body)
		return h.HandleMerged(payload)
	})

	registry.Register(TypeMergeFailed, func(msg *drums.Message) error {
		payload := ParseMergeFailedPayload(msg.Body)
		return h.HandleMergeFailed(payload)
	})

	registry.Register(TypeReworkRequest, func(msg *drums.Message) error {
		payload := ParseReworkRequestPayload(msg.Body)
		return h.HandleReworkRequest(payload)
	})

	return registry
}

// WrapForgeHandlers creates drums handlers from a ForgeHandler.
func WrapForgeHandlers(h ForgeHandler) *HandlerRegistry {
	registry := NewHandlerRegistry()

	registry.Register(TypeMergeReady, func(msg *drums.Message) error {
		payload := ParseMergeReadyPayload(msg.Body)
		return h.HandleMergeReady(payload)
	})

	return registry
}

// ProcessProtocolMessage processes a protocol message using the registry.
// It returns (true, nil) if the message was handled successfully,
// (true, error) if handling failed, or (false, nil) if not a protocol message.
func (r *HandlerRegistry) ProcessProtocolMessage(msg *drums.Message) (bool, error) {
	if !IsProtocolMessage(msg.Subject) {
		return false, nil
	}

	if !r.CanHandle(msg) {
		return false, nil
	}

	err := r.Handle(msg)
	return true, err
}
