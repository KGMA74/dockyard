package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"dockyard/internal/events"

	"github.com/labstack/echo/v4"
)

// Events streams registry push notifications over SSE. It works the same in
// embedded and proxy mode since both v2 handlers publish to the same hub.
func Events(hub *events.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		w := c.Response()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		w.Flush()

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		// Keeps the connection alive through proxies/load balancers that
		// close idle streams.
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		ctx := c.Request().Context()
		for {
			select {
			case <-ctx.Done():
				return nil

			case <-ticker.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return nil
				}
				w.Flush()

			case ev, ok := <-ch:
				if !ok {
					return nil
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
					return nil
				}
				w.Flush()
			}
		}
	}
}
