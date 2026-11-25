package platform

import (
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/web"
)

// eventTopics are the hub topics the web app hears. A module that publishes on
// a new topic adds it here, or the event goes nowhere.
var eventTopics = []string{"download", "scan", "source", "library", "change", "run", "suggestions", "inbox"}

// events streams the hub's events to the web app. A client that reconnects
// reloads what it shows, so nothing is replayed.
func (a *App) events(c *echo.Context) error {
	ch, stop := a.Hub.Subscribe(eventTopics...)
	defer stop()
	st, err := web.OpenStream(c)
	if err != nil {
		return nil
	}
	interval := a.EventKeepAlive
	if interval <= 0 {
		interval = web.KeepAlive
	}
	keep := time.NewTicker(interval)
	defer keep.Stop()
	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-a.closing:
			return nil
		case e, ok := <-ch:
			if !ok || st.Send(e.Topic, "", e.Data) != nil {
				return nil
			}
		case <-keep.C:
			if st.Ping() != nil {
				return nil
			}
		}
	}
}
