package numberotp

import "encoding/json"

// JSONObject is the decoded body of endpoints without a dedicated model.
type JSONObject map[string]any

// SMS is one message received on a number.
type SMS struct {
	Code       string `json:"code"`
	Text       string `json:"text"`
	ReceivedAt string `json:"received_at"`
}

// Activation is a one-time number.
type Activation struct {
	ID          string `json:"id"`
	PhoneNumber string `json:"phone_number"`
	Service     string `json:"service"`
	ServiceName string `json:"service_name"`
	Country     string `json:"country"`
	// Status: waiting, received, finished, canceled or expired.
	Status string `json:"status"`
	// OTP is the code, once one arrived.
	OTP       *string `json:"otp"`
	FullSMS   *string `json:"full_sms"`
	SMS       []SMS   `json:"sms"`
	Cost      float64 `json:"cost"`
	Pool      string  `json:"pool"`
	ExpiresAt string  `json:"expires_at"`
	CreatedAt string  `json:"created_at"`
}

// Code returns the OTP, or "" if none yet.
func (a *Activation) Code() string {
	if a == nil || a.OTP == nil {
		return ""
	}
	return *a.OTP
}

type ActivationResult struct {
	Success bool       `json:"success"`
	Data    Activation `json:"data"`
}

type CreateActivationResult struct {
	Success bool `json:"success"`
	Data    struct {
		ID          string `json:"id"`
		PhoneNumber string `json:"phoneNumber"`
		ExpiresAt   string `json:"expiresAt"`
		Pool        string `json:"pool"`
	} `json:"data"`
	// Balance left after the purchase.
	Balance float64 `json:"balance"`
}

type WaitResult struct {
	Success bool `json:"success"`
	Data    struct {
		OTP     string `json:"otp"`
		FullSMS string `json:"full_sms"`
		Status  string `json:"status"`
	} `json:"data"`
}

type ListResult struct {
	Success bool         `json:"success"`
	Data    []JSONObject `json:"data"`
}

type CreateRentalResult struct {
	Success bool `json:"success"`
	Data    struct {
		ID            string `json:"id"`
		PhoneNumber   string `json:"phoneNumber"`
		ExpiresAt     string `json:"expiresAt"`
		DurationHours int    `json:"durationHours"`
	} `json:"data"`
	Balance float64 `json:"balance"`
}

type Rental struct {
	ID                string `json:"id"`
	PhoneNumber       string `json:"phoneNumber"`
	Service           string `json:"service"`
	ServiceName       string `json:"serviceName"`
	Country           string `json:"country"`
	Status            string `json:"status"`
	ExpiresAt         string `json:"expiresAt"`
	RentDurationHours int    `json:"rentDurationHours"`
	SMSHistory        []struct {
		Code       string `json:"code"`
		Text       string `json:"text"`
		ReceivedAt string `json:"receivedAt"`
	} `json:"smsHistory"`
}

type RentalResult struct {
	Success bool   `json:"success"`
	Data    Rental `json:"data"`
}

type ExtendOptionsResult struct {
	Success bool `json:"success"`
	// Data maps extra hours to price (USD).
	Data map[string]float64 `json:"data"`
}

type Loyalty struct {
	Level             int     `json:"level"`
	Label             string  `json:"label"`
	Discount          float64 `json:"discount"`
	WeeklyActivations int     `json:"weekly_activations"`
	WeeklyTopup       float64 `json:"weekly_topup"`
	ProgressPct       float64 `json:"progress_pct"`
	NextTier          *struct {
		Level    int     `json:"level"`
		Label    string  `json:"label"`
		Discount float64 `json:"discount"`
	} `json:"next_tier"`
}

type Me struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	Name          *string `json:"name"`
	EmailVerified bool    `json:"email_verified"`
	CreatedAt     string  `json:"created_at"`
	Balance       float64 `json:"balance"`
	// OnHold is charged for numbers still waiting for an SMS — refunded if none arrives.
	OnHold       float64  `json:"on_hold"`
	Currency     string   `json:"currency"`
	OpenNumbers  int      `json:"open_numbers"`
	Loyalty      *Loyalty `json:"loyalty"`
	Restrictions struct {
		PurchasesOnHold bool `json:"purchases_on_hold"`
	} `json:"restrictions"`
	Auth struct {
		Via    string   `json:"via"`
		Scopes []string `json:"scopes"`
	} `json:"auth"`
}

type MeResult struct {
	Success bool `json:"success"`
	Data    Me   `json:"data"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	SessionID    string `json:"session_id"`
	NewUser      *bool  `json:"new_user,omitempty"`
}

type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type ClientSession struct {
	ID         string   `json:"id"`
	Platform   string   `json:"platform"`
	DeviceName string   `json:"device_name"`
	Scopes     []string `json:"scopes"`
	LastSeenAt string   `json:"last_seen_at"`
	CreatedAt  string   `json:"created_at"`
	Current    bool     `json:"current"`
}

type SessionList struct {
	Success bool            `json:"success"`
	Data    []ClientSession `json:"data"`
}

type ServiceList struct {
	Success bool `json:"success"`
	Data    struct {
		Services []struct {
			Code string `json:"code"`
			Name string `json:"name"`
		} `json:"services"`
	} `json:"data"`
}

type ServiceMatch struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Score int    `json:"score"`
}

type ServiceSearchResult struct {
	Success bool `json:"success"`
	Query   struct {
		Input     string `json:"input"`
		MatchedAs string `json:"matched_as"`
	} `json:"query"`
	Data []ServiceMatch `json:"data"`
	// Fallback is offered when nothing matched: the catch-all "any other service".
	Fallback *ServiceMatch `json:"fallback,omitempty"`
}

type Country struct {
	ID      int    `json:"id"`
	Name    string `json:"eng"`
	Visible int    `json:"visible"`
	Rent    int    `json:"rent"`
}

type CountryList struct {
	Success bool `json:"success"`
	// Data is keyed by country id.
	Data map[string]Country `json:"data"`
}

type PoolOffer struct {
	Cost    float64  `json:"cost"`
	Count   int      `json:"count"`
	Quality *float64 `json:"quality,omitempty"`
}

type PriceEntry struct {
	Cost  float64 `json:"cost"`
	Count int     `json:"count"`
	// Quality is the measured delivery rate (0–1) of the pool "auto" picks.
	Quality      float64              `json:"quality"`
	Pool         string               `json:"pool"`
	Pools        map[string]PoolOffer `json:"pools,omitempty"`
	Personalized bool                 `json:"personalized,omitempty"`
}

type PriceList struct {
	Success bool `json:"success"`
	// Data[countryID][serviceCode]
	Data map[string]map[string]PriceEntry `json:"data"`
}

type RentPriceList struct {
	Success bool `json:"success"`
	// Data[countryID][hours]
	Data map[string]map[string]struct {
		Cost  float64 `json:"cost"`
		Count int     `json:"count"`
	} `json:"data"`
}

type TopupStarted struct {
	Success bool `json:"success"`
	// URL opens the checkout in a browser.
	URL           string `json:"url"`
	TransactionID string `json:"transaction_id"`
	InvoiceID     string `json:"invoiceId,omitempty"`
}

type Topup struct {
	ID        string  `json:"id"`
	Amount    float64 `json:"amount"`
	Method    string  `json:"method"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
}

type TopupResult struct {
	Success bool  `json:"success"`
	Data    Topup `json:"data"`
}

// Event is a live account event (see Client.StreamEvents).
type Event struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Initiator string `json:"initiator,omitempty"`
	Refunded  *bool  `json:"refunded,omitempty"`
	// Data is the activation (activation.* / rental.*), {transaction_id, amount, method} (topup.completed) or {reminder_id, service, service_name, country, country_name} (reminder.back_in_stock).
	Data json.RawMessage `json:"data"`
	// StreamID is the position in your event stream — resume after it.
	StreamID string `json:"stream_id,omitempty"`
}

// Activation decodes Data for activation.* and rental.* events.
func (e Event) Activation() (*Activation, error) {
	var a Activation
	if err := json.Unmarshal(e.Data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

type EventPoll struct {
	Success bool    `json:"success"`
	Data    []Event `json:"data"`
	Cursor  string  `json:"cursor"`
}

// IapTopupResult is returned by Topups.Apple and Topups.Google.
type IapTopupResult struct {
	Success bool `json:"success"`
	Data    struct {
		Status        string  `json:"status"` // credited | already_credited
		Amount        float64 `json:"amount"`
		Balance       float64 `json:"balance"`
		TransactionID string  `json:"transaction_id"`
	} `json:"data"`
}

// AppConfig is what the mobile app needs at start (GET /app/config).
type AppConfig struct {
	MinVersion    map[string]string  `json:"min_version"`
	LatestVersion map[string]*string `json:"latest_version"`
	StoreURLs     map[string]*string `json:"store_urls"`
	Maintenance   *struct {
		Message string `json:"message"`
	} `json:"maintenance"`
	IAP struct {
		IOS      bool `json:"ios"`
		Android  bool `json:"android"`
		Products []struct {
			ID       string  `json:"id"`
			Credit   float64 `json:"credit"`
			PriceUSD float64 `json:"price_usd"`
		} `json:"products"`
	} `json:"iap"`
	WebTopup struct {
		URL            string   `json:"url"`
		IOSStorefronts []string `json:"ios_storefronts"`
		AndroidRegions []string `json:"android_regions"`
	} `json:"web_topup"`
	Links map[string]string `json:"links"`
}

// AppConfigResult wraps AppConfig.
type AppConfigResult struct {
	Success bool      `json:"success"`
	Data    AppConfig `json:"data"`
}
