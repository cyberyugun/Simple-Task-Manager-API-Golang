package worker

import (
	"fmt"
	"net/http"
	"sort"
)

func (p *Processor) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		metrics := p.Metrics()
		keys := make([]string, 0, len(metrics))
		for key := range metrics {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			_, _ = fmt.Fprintf(w, "task_worker_%s %d\n", key, metrics[key])
		}
	})
}
