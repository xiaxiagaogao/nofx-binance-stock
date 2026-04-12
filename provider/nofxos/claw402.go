package nofxos

// ============================================================================
// Claw402 Data Client (Optional Payment Gateway)
// ============================================================================
// Claw402 is an optional payment proxy for NofxOS data API.
// When configured, data requests are routed through the claw402 gateway
// instead of calling nofxos.ai directly. This is used for premium data access.

// Claw402Logger is the interface expected by the claw402 client for logging.
type Claw402Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Claw402DataClient is the claw402 payment gateway client
type Claw402DataClient struct {
	BaseURL   string
	WalletKey string
	Logger    Claw402Logger
}

// NewClaw402DataClient creates a new claw402 data client
func NewClaw402DataClient(baseURL, walletKey string, logger Claw402Logger) (*Claw402DataClient, error) {
	if baseURL == "" {
		baseURL = "https://claw402.ai"
	}
	return &Claw402DataClient{
		BaseURL:   baseURL,
		WalletKey: walletKey,
		Logger:    logger,
	}, nil
}

// SetClaw402 sets the claw402 client on the NofxOS client.
// When set, data requests will be routed through claw402 payment gateway.
func (c *Client) SetClaw402(claw402 *Claw402DataClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Store reference; the doRequest method can check for claw402 routing
	// For now this is a no-op stub — direct nofxos.ai access is used
	_ = claw402
}
