package cons

// Messages mirror pkg/enum/error.go — keep them in sync.

const (
	// ── Permission & authorization ────────────────────────────────
	NoPermission               = "You don't have permission for perform action"
	RecordNotFound             = "Record Not Found"
	PermissionDenied           = "permission denied"
	AuthRequired               = "authentication required"
	AuthNotExist               = "authentication not exist"
	AuthTokenInvalid           = "authentication token is invalid"
	AuthorizationNotValid      = "authorization not valid"
	AuthorizationCodeRequired  = "authorization code is required"
	LoginSessionNotValid       = "login session is not valid"
	TooManyFailedLoginAttempts = "too many failed login attempts. try again in %d minutes"

	// ── Token / session ───────────────────────────────────────────
	ErrInvalidToken        = "Invalid Token"
	BearerTokenFormat      = "format must be: Bearer <token>"
	TokenNotFoundOrRevoked = "token not found or already revoked"

	// ── Validation ────────────────────────────────────────────────
	EmailNotValid    = "error! Email not valid "
	IsRequiredSuffix = " is required"

	// ── Payment / Stripe ──────────────────────────────────────────
	InvalidAmount            = "invalid amount"
	InvalidDestinationAmount = "invalid destination amount"
	InvalidAccountNumber     = "invalid account number"
	InvalidPaymentIntent     = "invalid payment intent"
	HasInvalidSubscriptionId = "you have invalid subscription Id "
	InvalidCard              = "invalid card"
	InvalidCardParams        = "invalid card params: exactly one of account or customer need to be set"
	DuplicateCard            = "duplicate card detected"
	StripeTokenIssue         = "you have issue with stripe token"
	PriceNotCreated          = "sorry! The price not created, please try again later"
	PaymentDeclined          = "error! the payment has been declined"
	SubscriptionDeclined     = "sorry! The subscription has declined to process, please try again later"
	NoSubscriptionDetail     = "sorry! no subscription detail"

	// ── Upload ────────────────────────────────────────────────────
	UnSupportedFileFormat   = "Unsupported image format"
	UploadRequiresMultipart = "upload requires multipart/form-data request"
	NoFilesFound            = "no files found"

	// ── Notification / message ────────────────────────────────────
	NotificationKeyNotExist        = "notification key does not exist"
	NotificationActionDisabled     = "notification action is disabled"
	UserNotificationActionDisabled = "user notification action is disabled"
	InvalidMessage                 = "invalid message"
	NoMessage                      = "no message"

	// ── Mail ──────────────────────────────────────────────────────
	MailjetApiKeyBlank    = "mailjet api_key should not blank"
	MailjetSecretKeyBlank = "mailjet api secret_key should not blank"
	MailTemplateNotLoaded = "mail template is not loaded"
	MailLayoutNotFound    = "mail layout not found"
	FromEmailOrNameBlank  = "from email or from name should not blank"

	// ── ActiveRecord ──────────────────────────────────────────────
	SlugifyEmptyTitle    = "slugify: empty title"
	CreateAttributesType = "Create: attributes must be map[string]any"
)
