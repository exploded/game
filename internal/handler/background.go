package handler

import (
	"context"
	"net/http"

	"github.com/exploded/game/internal/jobs"
)

// afterRequest runs job once the request has been answered, for work that
// can take longer than a request may, such as queuing an email to every
// member. Inside the request the throttle's deadline (requestDeadline) could
// cut the loop off part way, and sending again would then email the first
// part twice. ctx keeps the request's values but not its deadline or
// cancellation. A panic in job is logged (jobs.Go).
func afterRequest(r *http.Request, name string, job func(ctx context.Context)) {
	ctx := context.WithoutCancel(r.Context())
	jobs.Go(name, func() { job(ctx) })
}
