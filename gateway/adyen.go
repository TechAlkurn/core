package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/TechAlkurn/core/config"
	"github.com/TechAlkurn/core/lib"
	"github.com/adyen/adyen-go-api-library/v21/src/adyen"
	"github.com/adyen/adyen-go-api-library/v21/src/common"
)

/* =========================================================
   SERVICE
========================================================= */

const (
	adyenCheckoutVersion = "v71"
	adyenTransferVersion = "v4"
	adyenDefaultCurrency = "USD"
)

var adyenHTTPClient = &http.Client{Timeout: 15 * time.Second}

type adyenService struct {
	Client          *adyen.APIClient
	MerchantAccount string
	HmacKey         string
	ApiKey          string

	checkoutURL string // base URL of the Checkout API, without trailing slash
	transferURL string // base URL of the Balance Platform Transfers API
}

// NewAdyen builds the Adyen service from config:
//
//	adyen_api_key, adyen_merchant_account, adyen_hmac_key
//	adyen_environment      "live" for production, anything else = test
//	adyen_live_url_prefix  required in live: the merchant-specific Checkout
//	                       prefix from Customer Area > Developers > API URLs
func NewAdyen() *adyenService {
	apiKey := config.GetString("adyen_api_key")
	live := strings.EqualFold(config.GetString("adyen_environment"), "live")
	livePrefix := config.GetString("adyen_live_url_prefix")

	env := common.TestEnv
	checkoutURL := "https://checkout-test.adyen.com/" + adyenCheckoutVersion
	transferURL := "https://balanceplatform-api-test.adyen.com/btl/" + adyenTransferVersion
	if live {
		env = common.LiveEnv
		checkoutURL = fmt.Sprintf("https://%s-checkout-live.adyenpayments.com/checkout/%s", livePrefix, adyenCheckoutVersion)
		transferURL = "https://balanceplatform-api-live.adyen.com/btl/" + adyenTransferVersion
	}

	client := adyen.NewClient(&common.Config{
		ApiKey:                apiKey,
		Environment:           env,
		LiveEndpointURLPrefix: livePrefix,
		Log4XXError:           true,
	})

	return &adyenService{
		Client:          client,
		ApiKey:          apiKey,
		MerchantAccount: config.GetString("adyen_merchant_account"),
		HmacKey:         config.GetString("adyen_hmac_key"),
		checkoutURL:     checkoutURL,
		transferURL:     transferURL,
	}
}

// post sends a JSON body to Adyen and returns the raw response body.
// Any non-2xx status is returned as an error carrying Adyen's message.
func (s *adyenService) post(ctx context.Context, url string, body any) ([]byte, error) {
	if s.ApiKey == "" {
		return nil, errors.New("adyen: adyen_api_key is not configured")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.ApiKey)

	resp, err := adyenHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("adyen error (%d): %s", resp.StatusCode, respBody)
	}
	return respBody, nil
}

// decode unmarshals an Adyen response; an empty body (e.g. 202/204) yields nil.
func decodeAdyen(body []byte) (any, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

/*
	=========================================================
	  CHECKOUT PAYMENT (CARD / WALLET)

=========================================================
*/

type AdyenAmount struct {
	Currency string `json:"currency"`
	Value    int64  `json:"value"` // minor units, e.g. 1000 = 10.00 USD
}

type AdyenPaymentRequest struct {
	Amount                   AdyenAmount       `json:"amount"`
	Reference                string            `json:"reference"`
	PaymentMethod            map[string]string `json:"paymentMethod"`
	ReturnURL                string            `json:"returnUrl"`
	MerchantAccount          string            `json:"merchantAccount"`
	ShopperReference         string            `json:"shopperReference,omitempty"`
	StorePaymentMethod       bool              `json:"storePaymentMethod,omitempty"`
	ShopperInteraction       string            `json:"shopperInteraction,omitempty"`
	RecurringProcessingModel string            `json:"recurringProcessingModel,omitempty"`
}

// adyenAmountFromForm reads "amount" (minor units) and "currency" from the form.
func adyenAmountFromForm(form map[string]any) (AdyenAmount, error) {
	value := lib.ToInt64(form["amount"])
	if value <= 0 {
		return AdyenAmount{}, errors.New("adyen: amount must be a positive value in minor units")
	}
	currency := strings.ToUpper(lib.ToString(form["currency"]))
	if currency == "" {
		currency = adyenDefaultCurrency
	}
	return AdyenAmount{Currency: currency, Value: value}, nil
}

func (s *adyenService) SaveCard(in Request) ([]byte, error) {
	return nil, errors.New("adyen: SaveCard is not implemented; use MakeCardPayment with store_payment_method")
}

func (s *adyenService) RetrieveCard(primaryId string) ([]byte, error) {
	return nil, errors.New("adyen: RetrieveCard is not implemented")
}

func (s *adyenService) RemoveCard(primaryId string) ([]byte, error) {
	return nil, errors.New("adyen: RemoveCard is not implemented")
}

// MakeCardPayment charges an encrypted card (Adyen Web/Drop-in fields).
//
// Form: reference, amount (minor units), currency, card_number, expiry_month,
// expiry_year, security_code; optional shopper_reference +
// store_payment_method=true to tokenize the card for later subscription use.
func (s *adyenService) MakeCardPayment(in Request) ([]byte, error) {
	form := lib.ToMapped(in.GetForm())

	amount, err := adyenAmountFromForm(form)
	if err != nil {
		return nil, err
	}

	reqBody := AdyenPaymentRequest{
		Amount:          amount,
		Reference:       lib.ToString(form["reference"]),
		ReturnURL:       config.GetString("adyen_payment_return_url"),
		MerchantAccount: s.MerchantAccount,
		PaymentMethod: map[string]string{
			"type":                  "scheme",
			"encryptedCardNumber":   lib.ToString(form["card_number"]),
			"encryptedExpiryMonth":  lib.ToString(form["expiry_month"]),
			"encryptedExpiryYear":   lib.ToString(form["expiry_year"]),
			"encryptedSecurityCode": lib.ToString(form["security_code"]),
		},
	}

	// First payment of a subscription: store the card as a token.
	if shopperRef := lib.ToString(form["shopper_reference"]); shopperRef != "" && lib.ToBool(form["store_payment_method"]) {
		reqBody.ShopperReference = shopperRef
		reqBody.StorePaymentMethod = true
		reqBody.ShopperInteraction = "Ecommerce"
		reqBody.RecurringProcessingModel = "Subscription"
	}

	return s.post(context.Background(), s.checkoutURL+"/payments", reqBody)
}

/*
	=========================================================
	  RECURRING / SUBSCRIPTION PAYMENT

=========================================================
*/

type AdyenSubscriptionPaymentResponse struct {
	Reference  string `json:"pspReference"`
	ResultCode string `json:"resultCode"`
}

// MakeSubscriptionPayment charges a previously stored card (merchant-initiated).
//
// Form: reference, amount (minor units), currency, shopper_reference,
// stored_payment_method_id.
func (s *adyenService) MakeSubscriptionPayment(in Request) ([]byte, error) {
	form := lib.ToMapped(in.GetForm())

	amount, err := adyenAmountFromForm(form)
	if err != nil {
		return nil, err
	}

	shopperRef := lib.ToString(form["shopper_reference"])
	storedId := lib.ToString(form["stored_payment_method_id"])
	if shopperRef == "" || storedId == "" {
		return nil, errors.New("adyen: shopper_reference and stored_payment_method_id are required")
	}

	reqBody := AdyenPaymentRequest{
		Amount:                   amount,
		Reference:                lib.ToString(form["reference"]),
		ShopperReference:         shopperRef,
		ReturnURL:                config.GetString("adyen_payment_return_url"),
		MerchantAccount:          s.MerchantAccount,
		ShopperInteraction:       "ContAuth",
		RecurringProcessingModel: "Subscription",
		PaymentMethod: map[string]string{
			"type":                  "scheme",
			"storedPaymentMethodId": storedId,
		},
	}

	return s.post(context.Background(), s.checkoutURL+"/payments", reqBody)
}

/*
	=========================================================
	  PAYOUT / TRANSFER (PLATFORMS)

=========================================================
*/

type Amount struct {
	Value    int64  `json:"value"`
	Currency string `json:"currency"`
}

type TransferCounterparty struct {
	TransferInstrumentId string `json:"transferInstrumentId"`
}

type Transfer struct {
	Amount                  Amount               `json:"amount"`
	BalanceAccountId        string               `json:"balanceAccountId"`
	Category                string               `json:"category"`
	Counterparty            TransferCounterparty `json:"counterparty"`
	Priority                string               `json:"priority,omitempty"`
	ReferenceForBeneficiary string               `json:"referenceForBeneficiary,omitempty"`
	Reference               string               `json:"reference,omitempty"`
	Description             string               `json:"description,omitempty"`
}

func (s *adyenService) Transfer(input *Transfer) (any, error) {
	if input == nil {
		return nil, errors.New("adyen: transfer input is required")
	}
	if input.Amount.Value <= 0 || input.Amount.Currency == "" {
		return nil, errors.New("adyen: transfer amount and currency are required")
	}
	if input.BalanceAccountId == "" || input.Counterparty.TransferInstrumentId == "" {
		return nil, errors.New("adyen: balanceAccountId and counterparty.transferInstrumentId are required")
	}

	body, err := s.post(context.Background(), s.transferURL+"/transfers", input)
	if err != nil {
		return nil, fmt.Errorf("adyen transfer: %w", err)
	}
	return decodeAdyen(body)
}

type CancelTransfersInput struct {
	TransferIds []string `json:"transferIds"`
}

func (s *adyenService) CancelTransfers(ctx context.Context, input *CancelTransfersInput) (any, error) {
	if input == nil || len(input.TransferIds) == 0 {
		return nil, errors.New("adyen: at least one transfer id is required")
	}

	body, err := s.post(ctx, s.transferURL+"/transfers/cancel", input)
	if err != nil {
		return nil, fmt.Errorf("adyen cancel transfer: %w", err)
	}
	return decodeAdyen(body)
}
