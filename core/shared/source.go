package shared

import (
	"context"
	"net/http"
)

// ApplicationInfo describes the process owning the client-side proxy connection.
type ApplicationInfo struct {
	Name string
	PID  uint32
}
type applicationContextKey struct{}

func WithApplication(ctx context.Context, info ApplicationInfo) context.Context {
	return context.WithValue(ctx, applicationContextKey{}, info)
}
func RequestApplication(r *http.Request) ApplicationInfo {
	info, _ := r.Context().Value(applicationContextKey{}).(ApplicationInfo)
	return info
}
