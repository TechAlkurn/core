package gateway

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/TechAlkurn/core/components/base"
	"github.com/TechAlkurn/core/cons"

	"github.com/TechAlkurn/core/config"

	"github.com/TechAlkurn/core/reqctx"

	"github.com/TechAlkurn/core/db/activerecord"
	"github.com/TechAlkurn/core/lib"
	"github.com/skip2/go-qrcode"

	"github.com/stripe/stripe-go/v79"
	"github.com/stripe/stripe-go/v79/account"
	"github.com/stripe/stripe-go/v79/card"
	"github.com/stripe/stripe-go/v79/charge"
	"github.com/stripe/stripe-go/v79/checkout/session"
	"github.com/stripe/stripe-go/v79/customer"
	"github.com/stripe/stripe-go/v79/invoice"
	"github.com/stripe/stripe-go/v79/oauth"
	"github.com/stripe/stripe-go/v79/paymentintent"
	"github.com/stripe/stripe-go/v79/paymentlink"
	"github.com/stripe/stripe-go/v79/paymentmethod"
	"github.com/stripe/stripe-go/v79/price"
	"github.com/stripe/stripe-go/v79/product"
	"github.com/stripe/stripe-go/v79/refund"
	"github.com/stripe/stripe-go/v79/source"
	"github.com/stripe/stripe-go/v79/subscription"
	"github.com/stripe/stripe-go/v79/terminal/connectiontoken"
	"github.com/stripe/stripe-go/v79/terminal/location"
	"github.com/stripe/stripe-go/v79/terminal/reader"
	testreader "github.com/stripe/stripe-go/v79/testhelpers/terminal/reader"
	"github.com/stripe/stripe-go/v79/token"
	"github.com/stripe/stripe-go/v79/transfer"
	"github.com/stripe/stripe-go/v79/webhookendpoint"
)

// Request is the gRPC request shape the form-driven methods read. The host's
// protoc-generated *gen.Request satisfies it, so this library never imports
// the host's generated package.
type Request interface {
	GetForm() []byte
	GetPrimaryId() uint32
	GetIdentifier() string
}

type StripePayment interface {
	LoadStripeKey()
	StripeCharge(in Request) (result *stripe.Charge, err error)
	StripeChargeWith3DS(in Request) (result *stripe.PaymentIntent, err error)
	RetrievePaymentIntent(paymentIntentId string) (result *stripe.PaymentIntent, err error)
	CreateCustomers(name, email string, address map[string]any) (result *stripe.Customer, err error)
	StripeCard(in Request) (result *stripe.Card, err error)
	CreateCard(name, email, token string, address map[string]any) (result *stripe.Card, err error)
	StripeCardList(customerID string) (result []*stripe.PaymentMethod, err error)
	RetrieveCard(primaryId, customerId string) (result *stripe.Card, err error)
	RemoveCard(primaryId, customerId string) (result *stripe.Card, err error)
	RetrieveSource(sourceId string) (result *stripe.Source, err error)
	RemoveSource(sourceId, customerId string) (result *stripe.Card, err error)
	StripeTransfers(amount int64, accountNumber string) (result *stripe.Transfer, err error)
	StripeCreateSubscription(in Request) (result *stripe.Subscription, err error)
	StripeCreateSubscriptionByPaymentIntent(in Request, stripePriceId, paymentIntentId string) (result *stripe.Subscription, err error)
	StripeSubscriptionUpdate(in Request) (result *stripe.Subscription, err error)
	StripeSubscriptionCancel(subscriptionId string) (result *stripe.Subscription, err error)
	StripeSubscriptionCancelAtPeriodEnd(subscriptionId string) (result *stripe.Subscription, err error)
	StripeSubscriptionDetail(subscriptionId string) (result *stripe.Subscription, err error)
	CreateCheckoutSession(input CheckoutSessionInput) (result *stripe.CheckoutSession, err error)
	RetrieveCheckoutSession(sessionId string) (result *stripe.CheckoutSession, err error)
	StripeProductList() (result []*stripe.Product, err error)
	StripePriceList() (result []*stripe.Price, err error)
	StripeCreateAccount(in Request) (result *stripe.Account, err error)
	StripeConnectedAccounts(in Request) (token *stripe.OAuthToken, err error)
	RetrieveAccounts(primaryId string) (result *stripe.Account, err error)
	StripeAccountDelete(primaryId string) (result *stripe.Account, err error)
	GenerateQRCode(url string) error
	StripeQR(amount, qty int64, url string, desc uint32) (result *stripe.PaymentLink, err error)
	CreatePaymentMethod(token string) (result *stripe.PaymentMethod, err error)
	PaymentLink(plink string) (result *stripe.PaymentLink, err error)
	StripeRefund(paymentId string, amount int64) (result *stripe.Refund, err error)

	/* Terminal (POS)*/
	StripePos(amount float64, currency string) (result any, err error)
	StripeChargeList(customerID string) (results []*stripe.Charge)
	ConnectionToken() (result *stripe.TerminalConnectionToken, err error)
	TerminalReaderLocation(in Request) (result *stripe.TerminalLocation, err error)
	TerminalReader(location, registration_code string) (*stripe.TerminalReader, error)
	PaymentIntent(amount float64, currency, customer, paymentMethod string) (*stripe.PaymentIntent, error)
	PaymentIntentList(customerID string) (results []*stripe.PaymentIntent)
	ProcessPaymentIntent(paymentIntent, readerId string) (result *stripe.TerminalReader, err error)
	PaymentIntentCapture(paymentIntent string) (result *stripe.PaymentIntent, err error)
	StripePaymentIntentCancel(paymentIntent string) (result *stripe.PaymentIntent, err error)
	StripePaymentIntentDetail(paymentIntent string) (result *stripe.PaymentIntent, err error)
	SimulateTerminalPayment(readerId string) (result *stripe.TerminalReader, err error)

	CreateWebhook(url string) (result *stripe.WebhookEndpoint, err error)
	StripeWebhook() (err error)
	StripeWebhookCreate() (err error)
}

type stripePayment struct {
	Key string
}

const piMetaSubscription = "membership_subscription_id"

/*
source (string):
	A payment source to be charged. This can be the ID of a card (i.e., credit or debit card), a bank account, a source, a token, or a connected account. For certain sources—namely, cards, bank accounts, and attached sources—you must also pass the ID of the associated customer.
*/

// NewRepository creates a new repositorys
func NewStripe() StripePayment {
	privateKey := config.GetString("stripe_private_key")
	stripe.Key = privateKey
	return &stripePayment{
		Key: privateKey,
	}
}

func (s *stripePayment) LoadStripeKey() {
	stripe.Key = fmt.Sprintf("%v", s.Key)
}

func (s *stripePayment) StripeConnectedAccounts(in Request) (token *stripe.OAuthToken, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	if item, ok := reqFrm["code"]; ok {
		item := lib.ToString(item)
		params := &stripe.OAuthTokenParams{
			GrantType: stripe.String("authorization_code"),
			Code:      stripe.String(item),
		}
		token, err = oauth.New(params)
		if err != nil {
			return nil, err
		}
		if err = activerecord.Exec("UPDATE user_profile SET stripe_account_number = ? WHERE user_id = ?", token.StripeUserID, reqctx.UserID()).Error; err != nil {
			return nil, err
		}
		// config.Log.Printf("token:%v\n", token)
		// config.Log.Printf("token StripeUserID:%v\n", token.StripeUserID)
		// result, err = account.GetByID(token.StripeUserID, &stripe.AccountParams{})
	}
	return
}

func (s *stripePayment) StripeCreateAccount(in Request) (result *stripe.Account, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	// str returns nil for missing/empty keys so stripe-go omits the param
	// instead of panicking on a nil interface (or sending "" to Stripe).
	str := func(key string) *string {
		if v := strings.TrimSpace(lib.ToString(reqFrm[key])); v != "" {
			return stripe.String(v)
		}
		return nil
	}

	var missing []string
	for _, key := range []string{"countryCode", "email"} {
		if str(key) == nil {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}

	var dOBParams *stripe.PersonDOBParams
	if itemDob, ok := reqFrm["dob"].(map[string]any); ok {
		dOBParams = &stripe.PersonDOBParams{}
		if item, ok := itemDob["day"]; ok {
			dOBParams.Day = stripe.Int64(int64(lib.ToUint32(item)))
		}
		if item, ok := itemDob["month"]; ok {
			dOBParams.Month = stripe.Int64(int64(lib.ToUint32(item)))
		}
		if item, ok := itemDob["year"]; ok {
			dOBParams.Year = stripe.Int64(int64(lib.ToUint32(item)))
		}
	}

	params := &stripe.AccountParams{
		Type:         stripe.String("custom"),
		BusinessType: str("business_type"),
		Country:      str("countryCode"),
		Email:        str("email"),
		BusinessProfile: &stripe.AccountBusinessProfileParams{
			Name: str("name"), // Business name
			URL:  str("url"),  // Business website
		},
		Capabilities: &stripe.AccountCapabilitiesParams{
			Transfers: &stripe.AccountCapabilitiesTransfersParams{Requested: stripe.Bool(true)},
		},
		Individual: &stripe.PersonParams{
			Address: &stripe.AddressParams{
				City:       str("city"),
				Country:    str("country"),
				Line1:      str("line1"),
				Line2:      str("line2"),
				PostalCode: str("postal_code"),
				State:      str("state"),
			},
			Gender:    str("gender"),
			SSNLast4:  str("ssn_last_4"),
			FirstName: str("first_name"),
			LastName:  str("last_name"),
			Email:     str("email"),
			Phone:     str("phone"),
			DOB:       dOBParams,
		},
		TOSAcceptance: &stripe.AccountTOSAcceptanceParams{
			Date: stripe.Int64(lib.Time()),
			IP:   str("ip_address"),
		},
	}
	// Only attach a bank account when one was supplied; Stripe rejects a
	// partial external_account object.
	if accountNumber := str("account_number"); accountNumber != nil {
		params.ExternalAccount = &stripe.AccountExternalAccountParams{
			AccountNumber:     accountNumber,
			AccountHolderName: str("account_holder_name"),
			AccountHolderType: str("account_holder_type"),
			Country:           str("country_code"),
			Currency:          str("currency"),
			RoutingNumber:     str("routing_number"),
		}
	}
	return account.New(params)
}

func (s *stripePayment) StripeAccountDelete(primaryId string) (result *stripe.Account, err error) {
	s.LoadStripeKey()
	params := &stripe.AccountParams{}
	result, err = account.Del(primaryId, params)
	return
}

// RetrieveAccounts fetches a connected account and confirms it is usable: the
// id must look like a Stripe account id ("acct_..."), the account must exist,
// must not be deleted, and Stripe must not have disabled it
// (requirements.disabled_reason is set when Stripe blocks the account, e.g.
// "rejected.fraud" or "requirements.past_due").
func (s *stripePayment) RetrieveAccounts(primaryId string) (result *stripe.Account, err error) {
	s.LoadStripeKey()
	primaryId = strings.TrimSpace(primaryId)
	if !strings.HasPrefix(primaryId, "acct_") {
		return nil, errors.New(cons.InvalidAccountNumber)
	}
	result, err = account.GetByID(primaryId, &stripe.AccountParams{})
	if err != nil {
		return nil, err
	}
	if result == nil || lib.IsEmpty(result.ID) || result.Deleted {
		return nil, errors.New(cons.InvalidAccountNumber)
	}
	if result.Requirements != nil && result.Requirements.DisabledReason != "" {
		return nil, fmt.Errorf("stripe account %s is disabled: %s", result.ID, result.Requirements.DisabledReason)
	}
	return result, nil
}

func (s *stripePayment) StripeCharge(in Request) (result *stripe.Charge, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	var amount int64 = 0
	if item, ok := reqFrm["amount"]; ok {
		amount = int64(lib.ToUint64(item))
	}
	var tokenId string
	var tokenCustomer string
	var description string
	var currency string
	// Comma-ok assertions: a non-string value must not panic the server.
	if token, ok := reqFrm["token"].(map[string]any); ok {
		tokenId, _ = token["id"].(string)
		tokenCustomer, _ = token["customer"].(string)
		currency, _ = token["currency"].(string)
		description, _ = token["description"].(string)
	}
	if item, ok := reqFrm["description"].(string); ok {
		description = item
	}
	if lib.IsEmpty(amount) {
		return nil, errors.New(cons.InvalidAmount)
	}
	if lib.IsEmpty(tokenId) {
		return nil, errors.New(cons.ErrInvalidToken)
	}
	amount = amount * 100
	sourceId := tokenId
	customerId := tokenCustomer

	// Create a new charge
	params := &stripe.ChargeParams{
		Amount:   stripe.Int64(amount),
		Currency: stripe.String(string(stripe.CurrencyUSD)),
	}

	if !lib.IsEmpty(currency) {
		params.Currency = stripe.String(currency)
	}
	if !lib.IsEmpty(description) {
		params.Description = stripe.String(description)
	}
	if !lib.IsEmpty(customerId) {
		params.Customer = stripe.String(customerId)
	}
	if !lib.IsEmpty(sourceId) {
		_ = params.SetSource(sourceId) // use a test card token provided by Stripe
	}
	/*
		Destination
	*/

	var destinationAmount int64
	var destinationAccountNumber string
	if item, ok := reqFrm["destination"]; ok {
		destination, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New(cons.InvalidAccountNumber)
		}
		if dist, ok := destination["amount"]; ok {
			destinationAmount = int64(lib.ToUint64(dist))
		}
		destinationAccountNumber, _ = destination["account_number"].(string)
		damount := int64(destinationAmount * 100)
		if lib.IsEmpty(damount) {
			return nil, errors.New(cons.InvalidDestinationAmount)
		}
		if lib.IsEmpty(destinationAccountNumber) {
			return nil, errors.New(cons.InvalidAccountNumber)
		}
		params.Destination = &stripe.ChargeDestinationParams{
			Account: stripe.String(destinationAccountNumber),
			Amount:  stripe.Int64(damount),
		}
	}

	result, err = charge.New(params)
	if err != nil {
		return nil, err
	}
	return
}

// StripeChargeWith3DS is the 3D-Secure-aware cousin of StripeCharge. It creates
// a PaymentIntent with Confirm=true against the supplied card token so Stripe
// can decide, per-card, whether a 3DS challenge is required.
//
// The returned PaymentIntent is in one of three terminal shapes:
//
//   - Status == "succeeded"        → funds captured; same outcome as a direct
//     Charge, no further action needed from the client.
//   - Status == "requires_action"  → the issuer wants 3DS. Hand the client the
//     ClientSecret and NextAction.RedirectToURL.URL; after the user completes
//     the challenge and returns to ReturnURL, the server must RetrievePaymentIntent
//     to see the final status.
//   - any other status             → the charge is NOT going through. Caller
//     should treat the PI as a soft decline and roll back any pending order
//     state, since the PI will never auto-advance to succeeded on its own.
//
// Fields read from the request form:
//
//	amount                 (required, integer or numeric string, dollars)
//	token.id               (required, Stripe card token like tok_visa)
//	token.customer         (optional, attach PI to this Customer)
//	token.currency         (optional, defaults to USD)
//	token.description      (optional, free-form)
//	description            (optional, overrides token.description)
//	return_url             (recommended, where Stripe sends the user after 3DS)
func (s *stripePayment) StripeChargeWith3DS(in Request) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	var amount int64
	if item, ok := reqFrm["amount"]; ok {
		amount = int64(lib.ToUint64(item))
	}
	var tokenId, tokenCustomer, description, currency, returnURL string
	if token, ok := reqFrm["token"].(map[string]any); ok {
		if item, ok := token["id"]; ok {
			tokenId, _ = item.(string)
		}
		if item, ok := token["customer"]; ok {
			tokenCustomer, _ = item.(string)
		}
		if item, ok := token["currency"]; ok {
			currency, _ = item.(string)
		}
		if item, ok := token["description"]; ok {
			description, _ = item.(string)
		}
	}
	if item, ok := reqFrm["description"]; ok {
		description, _ = item.(string)
	}
	if item, ok := reqFrm["return_url"]; ok {
		returnURL, _ = item.(string)
	}
	if lib.IsEmpty(returnURL) {
		returnURL = config.GetString("stripe_default_return_url")
	}
	if lib.IsEmpty(amount) {
		return nil, errors.New(cons.InvalidAmount)
	}
	if lib.IsEmpty(tokenId) {
		return nil, errors.New(cons.ErrInvalidToken)
	}
	if currency == "" {
		currency = string(stripe.CurrencyUSD)
	}
	amount = amount * 100

	// A raw card token has to be exchanged for a PaymentMethod before it can be
	// attached to a PaymentIntent — PaymentIntents don't accept `source` the way
	// the legacy Charge API does.
	pm, err := s.CreatePaymentMethod(tokenId)
	if err != nil {
		return nil, err
	}
	params := &stripe.PaymentIntentParams{
		Amount:             stripe.Int64(amount),
		Currency:           stripe.String(currency),
		PaymentMethod:      stripe.String(pm.ID),
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		Confirm:            stripe.Bool(true),
	}
	if !lib.IsEmpty(returnURL) {
		params.ReturnURL = stripe.String(returnURL)
	}
	if !lib.IsEmpty(tokenCustomer) {
		params.Customer = stripe.String(tokenCustomer)
	}
	if !lib.IsEmpty(description) {
		params.Description = stripe.String(description)
	}
	result, err = paymentintent.New(params)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RetrievePaymentIntent re-fetches a PaymentIntent after the client has
// completed (or abandoned) the 3DS challenge. The caller MUST re-read status
// from Stripe here — never trust a client-reported outcome — before marking
// the order paid. A successful auth lands on Status=="succeeded"; a failed
// challenge typically lands on "requires_payment_method" (user can retry
// with another card) or "canceled".
func (s *stripePayment) RetrievePaymentIntent(paymentIntentId string) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	result, err = paymentintent.Get(paymentIntentId, nil)
	return
}

// stripeSearchEscape escapes a value for use inside a single-quoted Stripe
// Search Query Language string: backslash first, then the quote.
var stripeSearchEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

func stripeSearchEscape(v string) string {
	return stripeSearchEscaper.Replace(v)
}

func (s *stripePayment) CreateCustomers(name, email string, address map[string]any) (result *stripe.Customer, err error) {
	s.LoadStripeKey()
	cparams := &stripe.CustomerSearchParams{
		SearchParams: stripe.SearchParams{
			Query: "name:'" + stripeSearchEscape(name) + "' AND email:'" + stripeSearchEscape(email) + "'",
			Limit: stripe.Int64(1),
		},
	}
	if customerIter := customer.Search(cparams); customerIter.Next() {
		result = customerIter.Customer()
		return
	}
	params := &stripe.CustomerParams{
		Name:  stripe.String(name),
		Email: stripe.String(email),
	}
	if lib.NotEmpty(address) {
		params.Address = &stripe.AddressParams{
			Line1:      stripe.String(lib.ToString(address["line1"])),
			Line2:      stripe.String(lib.ToString(address["line2"])),
			City:       stripe.String(lib.ToString(address["city"])),
			State:      stripe.String(lib.ToString(address["state"])),
			Country:    stripe.String(lib.ToString(address["country"])),
			PostalCode: stripe.String(lib.ToString(address["postal_code"])),
		}
	}
	result, err = customer.New(params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) StripeCardList(customerID string) (result []*stripe.PaymentMethod, err error) {
	s.LoadStripeKey()
	params := &stripe.PaymentMethodListParams{
		Type:     stripe.String(string(stripe.PaymentMethodTypeCard)),
		Customer: stripe.String(customerID),
	}
	params.Limit = stripe.Int64(25)
	iter := paymentmethod.List(params)
	for iter.Next() {
		result = append(result, iter.PaymentMethod())
	}
	err = iter.Err()
	return
}

func (s *stripePayment) _duplicateCardDetected(customerID, tokenID string) (flag bool) {
	s.LoadStripeKey()
	tok, err := token.Get(tokenID, nil)
	if err != nil || tok.Card == nil {
		return false
	}
	items, _ := s.StripeCardList(customerID)
	for _, item := range items {
		if item.Card != nil && item.Card.Fingerprint == tok.Card.Fingerprint {
			return true
		}
	}
	return false
}

// StripeCard creates a card (in.GetPrimaryId() == 0, "token" required) or updates
// the existing card in.GetIdentifier() for "customer" (in.GetPrimaryId() != 0). All form
// reads are nil-safe: a missing key yields "" instead of panicking.
func (s *stripePayment) StripeCard(in Request) (result *stripe.Card, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	name := lib.ToString(reqFrm["name"])
	email := lib.ToString(reqFrm["email"])
	token := lib.ToString(reqFrm["token"])
	customer := lib.ToString(reqFrm["customer"])
	expMonth := lib.ToString(reqFrm["exp_month"])
	expYear := lib.ToString(reqFrm["exp_year"])

	if lib.IsEmpty(in.GetPrimaryId()) {
		if token == "" {
			return nil, errors.New(cons.InvalidCard)
		}
		address, _ := reqFrm["address"].(map[string]any)
		result, err = s.CreateCard(name, email, token, address)
	} else {
		if lib.IsEmpty(customer) || lib.IsEmpty(in.GetIdentifier()) {
			return nil, errors.New(cons.InvalidCardParams)
		}
		params := &stripe.CardParams{Customer: stripe.String(customer)}
		if name != "" {
			params.Name = stripe.String(name)
		}
		if expMonth != "" {
			params.ExpMonth = stripe.String(expMonth)
		}
		if expYear != "" {
			params.ExpYear = stripe.String(expYear)
		}
		result, err = s._updateCard(in.GetIdentifier(), params)
	}
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) CreateCard(name, email, tokenID string, address map[string]any) (result *stripe.Card, err error) {
	s.LoadStripeKey()
	customer, err := s.CreateCustomers(name, email, address)
	if err != nil {
		return nil, err
	}
	if isDuplicate := s._duplicateCardDetected(customer.ID, tokenID); isDuplicate {
		return nil, errors.New(cons.DuplicateCard)
	}
	paramsCard := &stripe.CardParams{
		Customer: stripe.String(customer.ID),
		Token:    stripe.String(tokenID),
	}
	result, err = card.New(paramsCard)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) _updateCard(primaryId string, params *stripe.CardParams) (result *stripe.Card, err error) {
	s.LoadStripeKey()
	result, err = card.Update(primaryId, params)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.InvalidCard)
	}
	return
}

func (s *stripePayment) RetrieveCard(primaryId, customerId string) (result *stripe.Card, err error) {
	s.LoadStripeKey()
	params := &stripe.CardParams{
		Customer: stripe.String(customerId),
	}
	result, err = card.Get(primaryId, params)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.InvalidCard)
	}
	return
}

func (s *stripePayment) RemoveCard(primaryId, customerId string) (result *stripe.Card, err error) {
	s.LoadStripeKey()
	params := &stripe.CardParams{
		Customer: stripe.String(customerId),
	}
	result, err = card.Del(primaryId, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) RetrieveSource(primaryId string) (result *stripe.Source, err error) {
	s.LoadStripeKey()
	params := &stripe.SourceParams{}
	result, err = source.Get(primaryId, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) RemoveSource(sourceId, customerId string) (result *stripe.Card, err error) {
	s.LoadStripeKey()
	params := &stripe.CardParams{Customer: stripe.String(customerId)}
	result, err = card.Del(sourceId, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) StripeTransfers(amount int64, accountNumber string) (result *stripe.Transfer, err error) {
	s.LoadStripeKey()
	param := &stripe.TransferParams{
		Amount:      stripe.Int64(amount),
		Currency:    stripe.String(string(stripe.CurrencyUSD)),
		Destination: stripe.String(accountNumber),
	}
	result, err = transfer.New(param)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) GenerateQRCode(url string) error {
	s.LoadStripeKey()
	err := qrcode.WriteFile(url, qrcode.Medium, 256, "qrcode.png")
	if err != nil {
		return err
	}
	fmt.Println("QR code generated and saved to qrcode.png")
	return nil
}

func (s *stripePayment) _createAPrice(amount int64, orderId uint32) (result *stripe.Price, err error) {
	s.LoadStripeKey()
	itemNumber := fmt.Sprintf("Order Items:%d", orderId)
	params := &stripe.PriceParams{
		Currency:    stripe.String(string(stripe.CurrencyUSD)),
		UnitAmount:  stripe.Int64(amount),
		ProductData: &stripe.PriceProductDataParams{Name: stripe.String(itemNumber)},
	}
	result, err = price.New(params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) StripeQR(amount, qty int64, url string, desc uint32) (result *stripe.PaymentLink, err error) {
	s.LoadStripeKey()
	res, err := s._createAPrice(amount, desc)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(res.ID) {
		return nil, errors.New(cons.PriceNotCreated)
	}

	params := &stripe.PaymentLinkParams{
		LineItems: []*stripe.PaymentLinkLineItemParams{
			{
				Price:    stripe.String(res.ID),
				Quantity: stripe.Int64(qty),
			},
		},
		AfterCompletion: &stripe.PaymentLinkAfterCompletionParams{
			Type: stripe.String(string(stripe.PaymentLinkAfterCompletionTypeRedirect)),
			Redirect: &stripe.PaymentLinkAfterCompletionRedirectParams{
				URL: stripe.String(url),
			},
		},
	}
	params.AddMetadata("order_id", fmt.Sprintf("%v", desc))
	result, err = paymentlink.New(params)
	if err != nil {
		return nil, err
	}
	return
}

// Assuming you have a function to create a payment method from a token
func (s *stripePayment) PaymentLink(plink string) (result *stripe.PaymentLink, err error) {
	s.LoadStripeKey()
	// Create a payment method for the customer
	params := &stripe.PaymentLinkParams{}
	result, err = paymentlink.Get(plink, params)
	if err != nil {
		return nil, err
	}
	return
}

// StripeRefund refunds a payment. paymentId is either a PaymentIntent ID
// ("pi_...") or a Charge ID ("ch_..."). amount is in the smallest currency
// unit (cents); 0 refunds the full remaining amount.
func (s *stripePayment) StripeRefund(paymentId string, amount int64) (result *stripe.Refund, err error) {
	s.LoadStripeKey()
	paymentId = strings.TrimSpace(paymentId)
	if paymentId == "" {
		return nil, errors.New("missing payment id for refund")
	}
	if amount < 0 {
		return nil, errors.New(cons.InvalidAmount)
	}
	params := &stripe.RefundParams{}
	switch {
	case strings.HasPrefix(paymentId, "pi_"):
		params.PaymentIntent = stripe.String(paymentId)
	case strings.HasPrefix(paymentId, "ch_"), strings.HasPrefix(paymentId, "py_"):
		params.Charge = stripe.String(paymentId)
	default:
		return nil, fmt.Errorf("unsupported payment id for refund: %s", paymentId)
	}
	if amount > 0 {
		params.Amount = stripe.Int64(amount)
	}
	result, err = refund.New(params)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *stripePayment) StripeCreateSubscriptionByPaymentIntent(in Request, stripePriceId, paymentIntentId string) (result *stripe.Subscription, err error) {
	s.LoadStripeKey() // loads stripe.Key
	stripePriceId = strings.TrimSpace(stripePriceId)
	paymentIntentId = strings.TrimSpace(paymentIntentId)
	if stripePriceId == "" {
		return nil, errors.New(cons.PlanNotValid)
	}
	if !strings.HasPrefix(paymentIntentId, "pi_") {
		return nil, errors.New(cons.InvalidPaymentIntent)
	}
	params := &stripe.PaymentIntentParams{}
	params.AddExpand("invoice")
	params.AddExpand("payment_method")
	pi, err := paymentintent.Get(paymentIntentId, params)
	if err != nil {
		return nil, err
	}
	if !lib.IsEmpty(pi.Metadata[piMetaSubscription]) {
		return nil, errors.New(cons.PaymentIntentUsed)
	}
	// A PaymentIntent tagged for another user must not buy this user a plan.
	if owner := pi.Metadata["user_id"]; owner != "" && owner != lib.ToString(reqctx.UserID()) {
		return nil, errors.New(cons.PaymentIntentUsed)
	}

	var sub *stripe.Subscription
	if pi.Invoice != nil && pi.Invoice.Subscription != nil {
		sub, err = s._subscriptionFromInvoiceIntent(pi, stripePriceId)
	} else {
		sub, err = s._subscriptionPaidByIntent(in, pi, stripePriceId)
	}
	if err != nil {
		return nil, err
	}

	mark := &stripe.PaymentIntentParams{}
	mark.AddMetadata(piMetaSubscription, sub.ID)
	mark.AddMetadata("user_id", lib.ToString(reqctx.UserID()))
	if _, err := paymentintent.Update(pi.ID, mark); err != nil {
		config.Log.Println("membership: could not mark payment intent", pi.ID, "as used:", err)
	}
	return sub, nil
}

// paymentIntentNotPaid says why a PaymentIntent can't pay for a membership,
// using Stripe's own status so the client knows what step is missing.
func paymentIntentNotPaid(pi *stripe.PaymentIntent) error {
	var hint string
	switch pi.Status {
	case stripe.PaymentIntentStatusRequiresPaymentMethod:
		hint = "no card has been added, or the last card was declined"
	case stripe.PaymentIntentStatusRequiresConfirmation:
		hint = "the payment has not been confirmed"
	case stripe.PaymentIntentStatusRequiresAction:
		hint = "the card needs 3D Secure authentication on the client first"
	case stripe.PaymentIntentStatusProcessing:
		hint = "the payment is still processing, try again shortly"
	case stripe.PaymentIntentStatusCanceled:
		hint = "the payment was cancelled"
	}
	if pi.LastPaymentError != nil && pi.LastPaymentError.Msg != "" {
		hint += ": " + pi.LastPaymentError.Msg
	}
	if hint == "" {
		return fmt.Errorf("%s (status %s)", cons.PaymentIntentNotPaid, pi.Status)
	}
	return fmt.Errorf("%s: %s (status %s)", cons.PaymentIntentNotPaid, hint, pi.Status)
}

// _subscriptionFromInvoiceIntent adopts the subscription whose first invoice
// the client paid with this PaymentIntent.
func (s *stripePayment) _subscriptionFromInvoiceIntent(pi *stripe.PaymentIntent, stripePriceId string) (*stripe.Subscription, error) {
	if pi.Status != stripe.PaymentIntentStatusSucceeded {
		return nil, paymentIntentNotPaid(pi)
	}
	sub, err := subscription.Get(pi.Invoice.Subscription.ID, nil)
	if err != nil {
		return nil, err
	}
	if !_subscriptionHasPrice(sub, stripePriceId) {
		return nil, errors.New(cons.PlanNotValid)
	}
	// The PaymentIntent and the subscription must belong to the same customer,
	// and an ended subscription can't be adopted.
	if pi.Customer != nil && sub.Customer != nil && pi.Customer.ID != sub.Customer.ID {
		return nil, errors.New(cons.PaymentIntentUsed)
	}
	if owner := sub.Metadata["user_id"]; owner != "" && owner != lib.ToString(reqctx.UserID()) {
		return nil, errors.New(cons.PaymentIntentUsed)
	}
	if sub.Status == stripe.SubscriptionStatusCanceled || sub.Status == stripe.SubscriptionStatusIncompleteExpired {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	return sub, nil
}

// _subscriptionPaidByIntent creates a subscription whose first invoice is
// settled by a standalone PaymentIntent. The card is saved on the customer
// so Stripe can charge it for renewals.
func (s *stripePayment) _subscriptionPaidByIntent(in Request, pi *stripe.PaymentIntent, stripePriceId string) (*stripe.Subscription, error) {
	// Card attached but never confirmed: confirm it here instead of failing.
	if pi.Status == stripe.PaymentIntentStatusRequiresConfirmation && pi.PaymentMethod != nil {
		cp := &stripe.PaymentIntentConfirmParams{}
		cp.AddExpand("payment_method")
		confirmed, err := paymentintent.Confirm(pi.ID, cp)
		if err != nil {
			return nil, err
		}
		pi = confirmed
	}
	if pi.Status != stripe.PaymentIntentStatusSucceeded && pi.Status != stripe.PaymentIntentStatusRequiresCapture {
		return nil, paymentIntentNotPaid(pi)
	}
	if pi.PaymentMethod == nil {
		return nil, errors.New(cons.PaymentDeclined)
	}

	customerId := ""
	if pi.Customer != nil {
		customerId = pi.Customer.ID
	}
	if lib.IsEmpty(customerId) {
		// Scan needs a pointer: Scan(user) on a nil map never fills it, so
		// this path always failed with "user not found".
		user := map[string]any{}
		if err := activerecord.Raw("SELECT username, email FROM `user` WHERE id = ? LIMIT 1", reqctx.UserID()).Scan(&user).Error; err != nil {
			return nil, err
		}
		if len(user) == 0 {
			return nil, errors.New(cons.UserNotFound)
		}
		var name, email string
		if item, ok := user["username"]; ok {
			name = lib.ToString(item)
		}
		if item, ok := user["email"]; ok {
			email = lib.ToString(item)
		}
		cus, err := s.CreateCustomers(name, email, nil)
		if err != nil {
			return nil, err
		}
		customerId = cus.ID
	}

	// Save the card for renewals. Stripe refuses this for a card that was
	// used without a customer and without setup_future_usage; the first
	// period is still paid, but renewals will need a new card.
	pmId := pi.PaymentMethod.ID
	if pi.PaymentMethod.Customer == nil || pi.PaymentMethod.Customer.ID != customerId {
		if _, err := paymentmethod.Attach(pmId, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerId)}); err != nil {
			config.Log.Println("membership: card from", pi.ID, "not saved for renewals:", err)
			pmId = ""
		}
	}

	params := &stripe.SubscriptionParams{
		Customer:        stripe.String(customerId),
		Items:           []*stripe.SubscriptionItemsParams{{Price: stripe.String(stripePriceId)}},
		PaymentBehavior: stripe.String("default_incomplete"),
	}
	if !lib.IsEmpty(pmId) {
		params.DefaultPaymentMethod = stripe.String(pmId)
	}
	// reqFrm := lib.ToMapped(in.GetForm())
	if reqFrm := base.ToMapped(in.GetForm()); reqFrm.Has("coupon") && !lib.IsEmpty(reqFrm.ToString("coupon")) {
		params.Coupon = stripe.String(reqFrm.ToString("coupon"))
	}
	params.AddMetadata("user_id", lib.ToString(reqctx.UserID()))
	params.AddMetadata("payment_intent_id", pi.ID)
	params.AddExpand("latest_invoice")
	sub, err := subscription.New(params)
	if err != nil {
		return nil, err
	}
	cancel := func() {
		if _, cErr := subscription.Cancel(sub.ID, nil); cErr != nil {
			config.Log.Println("membership: could not cancel unpaid subscription", sub.ID, ":", cErr)
		}
	}

	inv := sub.LatestInvoice
	if inv == nil {
		cancel()
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	// The payment must cover the first invoice (after coupon and tax).
	if inv.Currency != pi.Currency || pi.Amount < inv.AmountDue {
		cancel()
		return nil, errors.New(cons.PaymentAmountMismatch)
	}
	if pi.Status == stripe.PaymentIntentStatusRequiresCapture {
		if _, err := paymentintent.Capture(pi.ID, nil); err != nil {
			cancel()
			return nil, err
		}
	}
	if inv.AmountDue > 0 && inv.Status != stripe.InvoiceStatusPaid {
		if _, err := invoice.Pay(inv.ID, &stripe.InvoicePayParams{PaidOutOfBand: stripe.Bool(true)}); err != nil {
			// The money is already taken: keep the subscription and log
			// enough to reconcile it by hand.
			config.Log.Println("membership: payment", pi.ID, "captured but invoice", inv.ID, "not marked paid:", err)
			return nil, err
		}
	}
	// Re-read so the status reflects the paid invoice (active / trialing).
	return subscription.Get(sub.ID, nil)
}

func _subscriptionHasPrice(sub *stripe.Subscription, stripePriceId string) bool {
	if sub == nil || sub.Items == nil {
		return false
	}
	for _, item := range sub.Items.Data {
		if item.Price != nil && item.Price.ID == stripePriceId {
			return true
		}
	}
	return false
}

func (s *stripePayment) StripeCreateSubscription(in Request) (result *stripe.Subscription, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	if lib.IsEmpty(reqFrm["token"]) {
		return nil, errors.New(cons.StripeTokenIssue)
	}
	// Comma-ok assertions / lib.ToString: malformed input must not panic.
	token, _ := reqFrm["token"].(map[string]any)
	if token == nil {
		return nil, errors.New(cons.StripeTokenIssue)
	}
	tokenId := lib.ToString(token["id"])
	tokenEmail := lib.ToString(token["email"])
	tokenCoupon := lib.ToString(token["coupon"])
	tokenName := lib.ToString(token["name"])
	if lib.IsEmpty(tokenId) {
		return nil, errors.New(cons.PaymentDeclined)
	}
	priceID := lib.ToString(reqFrm["price_id"])
	if lib.IsEmpty(priceID) {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	customerId := lib.ToString(reqFrm["customer_id"])
	pmId := lib.ToString(reqFrm["pm_id"])
	if lib.IsEmpty(customerId) {
		if lib.IsEmpty(tokenEmail) {
			return nil, errors.New(cons.EmailNotValid)
		}
		address, _ := token["address"].(map[string]any)
		if address == nil {
			address = make(map[string]any)
		}
		customer, err := s.CreateCustomers(tokenName, tokenEmail, address)
		if err != nil {
			return nil, err
		}
		customerId = customer.ID
	}
	if lib.IsEmpty(pmId) {
		pm, err := s.CreatePaymentMethod(tokenId)
		if err != nil {
			// Handle the error
			return nil, err
		}
		pmId = pm.ID
	}
	isProration := false
	if _, ok := reqFrm["is_proration"]; ok {
		isProration = true
	}

	_, err = paymentmethod.Attach(pmId, &stripe.PaymentMethodAttachParams{Customer: stripe.String(customerId)})
	if err != nil {
		// Handle the error
		return nil, err
	}

	// Create a subscription for the customer
	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerId),
		Items: []*stripe.SubscriptionItemsParams{
			{
				Plan: stripe.String(priceID),
			},
		},
		// ProrationBehavior: stripe.String("create_prorations"),
		// ProrationDate:     stripe.Int64(time.Now().Unix()),
		// DefaultSource: stripe.String(pmId),
		DefaultPaymentMethod: stripe.String(pmId),
	}
	if !lib.IsEmpty(tokenCoupon) {
		params.Coupon = stripe.String(tokenCoupon)
	}
	if isProration {
		params.ProrationBehavior = stripe.String("create_prorations")
	}
	result, err = subscription.New(params)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	return result, err
}

/* func (s *stripePayment) StripeSubscriptionUpdate(subscriptionId, sourcePlan string) (result *stripe.Subscription, err error) { */
func (s *stripePayment) StripeSubscriptionUpdate(in Request) (result *stripe.Subscription, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	subscriptionId := lib.ToString(reqFrm["subscription_id"])
	if lib.IsEmpty(subscriptionId) {
		return nil, errors.New(cons.HasInvalidSubscriptionId)
	}
	sub, err := subscription.Get(subscriptionId, nil)
	if err != nil {
		return nil, err
	}
	cancelAtPeriodEnd := false

	params := &stripe.SubscriptionParams{
		CancelAtPeriodEnd: &cancelAtPeriodEnd,
	}
	if priceId := lib.ToString(reqFrm["price_id"]); !lib.IsEmpty(priceId) {
		if sub.Items == nil || len(sub.Items.Data) == 0 {
			return nil, errors.New(cons.HasInvalidSubscriptionId)
		}
		params.Items = []*stripe.SubscriptionItemsParams{
			{
				ID:   stripe.String(sub.Items.Data[0].ID),
				Plan: stripe.String(priceId),
			},
		}
	}
	if item, ok := reqFrm["trial_end"]; ok && !lib.IsEmpty(item) {
		trialEnd := lib.ToUint64(item)
		params.TrialEnd = stripe.Int64(int64(trialEnd))
	}
	result, err = subscription.Update(subscriptionId, params)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	return
}

func (s *stripePayment) StripeSubscriptionCancel(subscriptionId string) (result *stripe.Subscription, err error) {
	s.LoadStripeKey()
	result, err = subscription.Cancel(subscriptionId, nil)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	return
}

// StripeSubscriptionCancelAtPeriodEnd schedules a subscription to end when the
// current billing period closes (Stripe `cancel_at_period_end = true`), leaving
// the membership active until then. Mirrors the PHP cancel flow.
func (s *stripePayment) StripeSubscriptionCancelAtPeriodEnd(subscriptionId string) (result *stripe.Subscription, err error) {
	s.LoadStripeKey()
	result, err = subscription.Update(subscriptionId, &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	return
}

func (s *stripePayment) StripeSubscriptionDetail(subscriptionId string) (result *stripe.Subscription, err error) {
	s.LoadStripeKey()
	result, err = subscription.Get(subscriptionId, nil)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.NoSubscriptionDetail)
	}
	return
}

// CheckoutSessionInput carries the fields needed to open a subscription-mode
// Stripe Checkout Session. TrialPeriodDays and Coupon are optional (0 / "").
type CheckoutSessionInput struct {
	CustomerId        string
	PriceId           string
	SuccessURL        string
	CancelURL         string
	ClientReferenceId string
	Coupon            string
	TrialPeriodDays   int64
}

// CreateCheckoutSession opens a subscription Checkout Session for the given
// customer and price. Mirrors the PHP `_stripeCheckoutSession` first-time flow.
func (s *stripePayment) CreateCheckoutSession(input CheckoutSessionInput) (result *stripe.CheckoutSession, err error) {
	s.LoadStripeKey()
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:   stripe.String(input.CustomerId),
		SuccessURL: stripe.String(input.SuccessURL),
		CancelURL:  stripe.String(input.CancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(input.PriceId),
				Quantity: stripe.Int64(1),
			},
		},
		AllowPromotionCodes: stripe.Bool(true),
	}
	if !lib.IsEmpty(input.ClientReferenceId) {
		params.ClientReferenceID = stripe.String(input.ClientReferenceId)
	}
	if !lib.IsEmpty(input.Coupon) {
		params.Discounts = []*stripe.CheckoutSessionDiscountParams{
			{Coupon: stripe.String(input.Coupon)},
		}
	}
	if input.TrialPeriodDays > 0 {
		params.SubscriptionData = &stripe.CheckoutSessionSubscriptionDataParams{
			TrialPeriodDays: stripe.Int64(input.TrialPeriodDays),
		}
	}
	result, err = session.New(params)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.SubscriptionDeclined)
	}
	return result, nil
}

// RetrieveCheckoutSession fetches a completed Checkout Session so the caller can
// read its resulting subscription id.
func (s *stripePayment) RetrieveCheckoutSession(sessionId string) (result *stripe.CheckoutSession, err error) {
	s.LoadStripeKey()
	result, err = session.Get(sessionId, nil)
	if err != nil {
		return nil, err
	}
	if lib.IsEmpty(result.ID) {
		return nil, errors.New(cons.NoSubscriptionDetail)
	}
	return result, nil
}

func (s *stripePayment) StripeProductList() (result []*stripe.Product, err error) {
	s.LoadStripeKey()
	params := &stripe.ProductListParams{}
	params.Limit = stripe.Int64(3)
	// Iterate instead of reading .ProductList().Data directly: that is nil
	// (and panics) when the API call fails, and it swallowed the error.
	iter := product.List(params)
	for len(result) < 3 && iter.Next() {
		result = append(result, iter.Product())
	}
	err = iter.Err()
	return
}

func (s *stripePayment) StripePriceList() (result []*stripe.Price, err error) {
	s.LoadStripeKey()
	params := &stripe.PriceListParams{}
	params.Limit = stripe.Int64(3)
	iter := price.List(params)
	for len(result) < 3 && iter.Next() {
		result = append(result, iter.Price())
	}
	err = iter.Err()
	return
}

/* Terminal (POS)*/

func (s *stripePayment) StripePos(amount float64, currency string) (result any, err error) {
	s.LoadStripeKey()
	token, err := s.ConnectionToken()
	if err != nil {
		return nil, err
	}
	pi, err := s.PaymentIntent(amount, currency, "", "")
	if err != nil {
		return nil, err
	}
	// Previously this logged the connection-token secret and returned nil.
	return map[string]any{
		"secret":         token.Secret,
		"payment_intent": pi,
	}, nil
}

func (s *stripePayment) ConnectionToken() (result *stripe.TerminalConnectionToken, err error) {
	s.LoadStripeKey()
	params := &stripe.TerminalConnectionTokenParams{}
	result, err = connectiontoken.New(params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) TerminalReader(location, registration_code string) (result *stripe.TerminalReader, err error) {
	s.LoadStripeKey()
	params := &stripe.TerminalReaderParams{
		Location:         stripe.String(location),
		RegistrationCode: stripe.String(registration_code),
	}
	result, err = reader.New(params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) StripeChargeList(customerID string) (results []*stripe.Charge) {
	s.LoadStripeKey()
	params := &stripe.ChargeListParams{
		Customer: stripe.String(customerID),
	}
	params.Limit = stripe.Int64(100)
	iter := charge.List(params)
	for iter.Next() {
		results = append(results, iter.Charge())
	}
	return
}

func (s *stripePayment) PaymentIntentList(customerID string) (results []*stripe.PaymentIntent) {
	s.LoadStripeKey()
	params := &stripe.PaymentIntentListParams{
		Customer: stripe.String(customerID),
	}
	params.Limit = stripe.Int64(100)
	iter := paymentintent.List(params)
	for iter.Next() {
		results = append(results, iter.PaymentIntent())
	}
	return
}

func (s *stripePayment) PaymentIntent(amount float64, currency, customer, paymentMethod string) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	if currency == "" {
		currency = "usd"
	}
	params := &stripe.PaymentIntentParams{
		// math.Round is required because floating-point calculations like
		// 19.99 * 100 can become 1998.999..., which would truncate to 1998
		// instead of 1999 and cause Stripe amount validation to fail.
		Amount:   stripe.Int64(int64(math.Round(amount * 100))),
		Currency: stripe.String(currency),
		/* PaymentMethodOptions: &stripe.PaymentIntentPaymentMethodOptionsParams{
			Card: &stripe.PaymentIntentPaymentMethodOptionsCardParams{
				RequestThreeDSecure: stripe.String("any"),
			},
		}, */
		PaymentMethodTypes: stripe.StringSlice([]string{
			"card_present",
			"card",
		}),
		CaptureMethod: stripe.String("manual"),
	}
	if !lib.IsEmpty(customer) {
		params.Customer = stripe.String(customer)
	}
	if !lib.IsEmpty(paymentMethod) {
		params.PaymentMethod = stripe.String(paymentMethod)
	}
	result, err = paymentintent.New(params)
	if err != nil {
		config.Log.Println(err)
		// Return Stripe's original error instead of hiding it behind
		// a generic "invalid payment intent" message.
		return nil, err
	}

	return
}

/*
func (s *stripePayment) PaymentIntent(amount float64, currency, customer, paymentMethod string) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	if currency == "" {
		currency = terminalCurrency()
	}

	// 	params := &stripe.PaymentIntentParams{
	// 	Amount:   stripe.Int64(amount * 100),
	// 	Currency: stripe.String(string(currency)),
	// 	AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
	// 		Enabled: stripe.Bool(true),
	// 	},
	// }

	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amount * 100),
		Currency: stripe.String(string(currency)),
		//   AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
		// 	Enabled: stripe.Bool(true),
		// },
		PaymentMethodOptions: &stripe.PaymentIntentPaymentMethodOptionsParams{
			Card: &stripe.PaymentIntentPaymentMethodOptionsCardParams{
				RequestThreeDSecure: stripe.String("any"),
			},
		},
		PaymentMethodTypes: stripe.StringSlice([]string{
			"card_present", "card",
		}),
		CaptureMethod: stripe.String("manual"), // manual,automatic
	}
	result, err = paymentintent.New(params)
	if err != nil {
		// config.Log.Println(err)
		return nil, stripeErrorMessage(err, cons.InvalidPaymentIntent)
	}
	return
} */

// terminalCurrency is the default currency for card_present PaymentIntents.
// Terminal only accepts the account country's local currency (GBP for a GB
// account), so this must not fall back to USD.
func TerminalCurrency() string {
	if c := os.Getenv("STRIPE_TERMINAL_CURRENCY"); c != "" {
		return c
	}
	return string(stripe.CurrencyGBP)
}

// stripeErrorMessage surfaces Stripe's own message (e.g. "currency usd is not
// supported in GB") instead of a generic fallback.
func StripeErrorMessage(err error, fallback string) error {
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) && stripeErr.Msg != "" {
		return errors.New(stripeErr.Msg)
	}
	return errors.New(fallback)
}

func (s *stripePayment) ProcessPaymentIntent(paymentIntent, readerId string) (result *stripe.TerminalReader, err error) {
	s.LoadStripeKey()
	params := &stripe.TerminalReaderProcessPaymentIntentParams{
		PaymentIntent: stripe.String(paymentIntent),
	}
	result, err = reader.ProcessPaymentIntent(readerId, params)
	if err != nil {
		return nil, err
	}
	return
}

// SimulateTerminalPayment fakes a card tap on a simulated reader (registered
// with registration_code "simulated-wpe"). Test mode only — call it after
// ProcessPaymentIntent has put the reader into its payment-collection state.
func (s *stripePayment) SimulateTerminalPayment(readerId string) (result *stripe.TerminalReader, err error) {
	s.LoadStripeKey()
	if !strings.HasPrefix(stripe.Key, "sk_test_") && !strings.HasPrefix(stripe.Key, "rk_test_") {
		return nil, errors.New("terminal simulation is only available in Stripe test mode")
	}
	params := &stripe.TestHelpersTerminalReaderPresentPaymentMethodParams{}
	result, err = testreader.PresentPaymentMethod(readerId, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) PaymentIntentCapture(paymentIntent string) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	params := &stripe.PaymentIntentCaptureParams{}
	result, err = paymentintent.Capture(paymentIntent, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) StripePaymentIntentCancel(paymentIntent string) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	params := &stripe.PaymentIntentCancelParams{}
	result, err = paymentintent.Cancel(paymentIntent, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) StripePaymentIntentDetail(paymentIntent string) (result *stripe.PaymentIntent, err error) {
	s.LoadStripeKey()
	params := &stripe.PaymentIntentParams{}
	result, err = paymentintent.Get(paymentIntent, params)
	if err != nil {
		return nil, err
	}
	return
}

func (s *stripePayment) TerminalReaderLocation(in Request) (result *stripe.TerminalLocation, err error) {
	s.LoadStripeKey()
	reqFrm := lib.ToMapped(in.GetForm())
	params := &stripe.TerminalLocationParams{
		DisplayName: stripe.String(lib.ToString(reqFrm["display_name"])),
		Address: &stripe.AddressParams{
			Line1:      stripe.String(lib.ToString(reqFrm["line1"])),
			City:       stripe.String(lib.ToString(reqFrm["city"])),
			State:      stripe.String(lib.ToString(reqFrm["state"])),
			Country:    stripe.String(lib.ToString(reqFrm["country"])),
			PostalCode: stripe.String(lib.ToString(reqFrm["postal_code"])),
		},
	}
	result, err = location.New(params)
	if err != nil {
		return nil, err
	}
	return
}

// Assuming you have a function to create a payment method from a token
func (s *stripePayment) CreatePaymentMethod(token string) (result *stripe.PaymentMethod, err error) {
	s.LoadStripeKey()
	// Create a payment method for the customer
	params := &stripe.PaymentMethodParams{
		Type: stripe.String("card"),
		Card: &stripe.PaymentMethodCardParams{
			Token: stripe.String(token), // Replace with a valid test card token
		},
	}
	result, err = paymentmethod.New(params)
	if err != nil {
		return nil, err
	}
	return
}

// Assuming you have a function to create a payment method from a token
func (s *stripePayment) CreateWebhook(url string) (result *stripe.WebhookEndpoint, err error) {
	s.LoadStripeKey()
	params := &stripe.WebhookEndpointParams{
		EnabledEvents: []*string{
			new("charge.succeeded"),
			new("charge.failed"),
			new("payment_intent.succeeded"),
			new("payment_method.attached"),
		},
		URL: new(url),
	}
	result, err = webhookendpoint.New(params)
	if err != nil {
		return nil, err
	}
	return
}

// Assuming you have a function to create a payment method from a token
func (s *stripePayment) StripeWebhook() (err error) {
	// Create a payment method for the customer

	return
}

// Assuming you have a function to create a payment method from a token
func (s *stripePayment) StripeWebhookCreate() (err error) {
	// Create a payment method for the customer

	return
}
