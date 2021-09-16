package stripe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

//
// Public types
//

// CLIAnalyticsEventContext is the structure that holds telemetry data sent to the Stripe Analytics Service
// this could be a problem if we are running multiple commands at once. I think we want to initialize this once and pass it along.
type CLIAnalyticsEventContext struct {
	UserAgent         string `url:"user_agent"`
	InvocationID      string `url:"invocation_id"`
	CommandPath       string `url:"command_path"`
	Merchant          string `url:"merchant"`
	CLIVersion        string `url:"cli_version"`
	OS                string `url:"os"`
	GeneratedResource bool   `url:"generated_resource"`
	RequestID         string `url:"request_id"`
	LiveMode          bool   `url:"livemode"`
}

// Add a public interface for the sendEvent

//
// Public functions
//

// GetAnalyticsEventContext returns the CLIAnalyticsEventContext instance (initializing it  first if necessary).
// add wait group counter here to keep track of all the telemetry we want to wait for.
//
func GetAnalyticsEventContext() *CLIAnalyticsEventContext {
	telemetrySync.Do(func() {
		telemetryInstance = &CLIAnalyticsEventContext{}
	})

	return telemetryInstance
}

//
// Private variables
//

var telemetryInstance *CLIAnalyticsEventContext
var telemetrySync sync.Once

// Private functions

// SetCommandContext sets the telemetry values for the command being executed.
// Needs to come from the gRPC method name.
func (e *CLIAnalyticsEventContext) SetCommandContext(cmd *cobra.Command) {
	e.CommandPath = cmd.CommandPath()
	e.GeneratedResource = false

	for _, value := range cmd.Annotations {
		// Generated commands have an annotation called "operation", we can
		// search for that to let us know it's generated
		if value == "operation" {
			e.GeneratedResource = true
		}
	}
}

// SetInvocationID sets the invocationId in context with an autogenerated UUID
func (e *CLIAnalyticsEventContext) SetInvocationID() {
	e.InvocationID = uuid.NewString()
}

// SendEvent sends a telemetry event to r.stripe.com
func (e *CLIAnalyticsEventContext) SendEvent(ctx context.Context, eventName string, eventValue string) (*http.Response, error) {
	time.Sleep(5 * time.Second)
	client := newTelemetryHTTPClient(false)

	if telemetryOptedOut(os.Getenv("STRIPE_CLI_TELEMETRY_OPTOUT")) {
		return nil, nil
	}

	analyticsURL, err := url.Parse("https://r.stripe.com/0")
	if err != nil {
		return nil, err
	}

	data, _ := query.Values(e)

	data.Set("client_id", "stripe-cli")
	data.Set("event_id", uuid.NewString())
	data.Set("event_name", eventName)
	data.Set("event_value", eventValue)
	data.Set("created", fmt.Sprint((time.Now().Unix())))

	req, err := http.NewRequest(http.MethodPost, analyticsURL.String(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("origin", "stripe-cli")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Sent telemetry event")

	return resp, nil
}

func newTelemetryHTTPClient(verbose bool) *http.Client {
	httpTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	tr := &verboseTransport{
		Transport: httpTransport,
		Verbose:   verbose,
		Out:       os.Stderr,
	}

	return &http.Client{
		Transport: tr,
	}
}
