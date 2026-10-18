# numberotp (Go)

The official Go client for the [NumberOTP API](https://www.numberotp.com/docs/api). It uses only the standard library and needs Go 1.24+.

```bash
go get github.com/numberotp/numberotp-go
```
Get an API key at [numberotp.com/dashboard/developer](https://www.numberotp.com/dashboard/developer) and set it as `NOTP_API_KEY`. Guides: [numberotp.com/docs/sdks](https://www.numberotp.com/docs/sdks).

```go
client := numberotp.NewClient() // reads NOTP_API_KEY

act, err := client.CreateAndWait(ctx,
	numberotp.CreateActivationParams{Service: "wa", Country: "16"},
	20*time.Minute,
	func(n *numberotp.CreateActivationResult) { fmt.Println("Use:", n.Data.PhoneNumber) })
if err != nil {
	var closed *numberotp.ActivationClosedError
	if errors.As(err, &closed) { /* expired — refunded */ }
}
fmt.Println("Code:", act.Code())
```

```go
client.Services.Search(ctx, &numberotp.SearchServicesParams{Q: "discord.com"})
client.Prices.Get(ctx, &numberotp.GetPricesParams{Service: numberotp.String("wa")})
client.Activations.Cancel(ctx, id) // full refund if no SMS yet
for ev, err := range client.StreamEvents(ctx, "") { … }   // live events
numberotp.VerifyWebhook(body, r.Header.Get("X-NumberOTP-Signature"), secret)
client.DeviceLogin(ctx, numberotp.DeviceLoginOptions{OnCode: func(d numberotp.DeviceCode) { … }})
```

Errors are `*numberotp.Error` values with `Status` and a stable `Code`. Purchases send an `Idempotency-Key`, and retries are only made where they're safe.
