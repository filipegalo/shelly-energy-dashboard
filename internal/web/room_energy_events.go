package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) roomEnergyEvents(w http.ResponseWriter, r *http.Request) {
	if s.roomEnergy == nil {
		http.Error(w, "room energy collector unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	stream, unsubscribe, err := s.roomEnergy.SubscribeRoomEnergy()
	if err != nil {
		http.Error(w, "too many room energy event streams", http.StatusServiceUnavailable)
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case event, open := <-stream:
			if !open {
				return
			}
			payload, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				s.logger.Error("encode room energy event", "operation", "sse", "error", marshalErr)
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
