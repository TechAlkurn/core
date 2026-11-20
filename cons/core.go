package cons

// Values mirror pkg/enum — keep them in sync.

const (
	SITE_MODE_LIVE       = "live"
	SITE_MODE_PRODUCTION = "production"
	SITE_MODE_STAGING    = "staging"
)

const (
	ADMIN_ID = 2 // super admin
)

/* User */
const (
	USER_STATUS_ACTIVE = 10
)

/* User message */
const (
	USER_MESSAGE_STATUS_READ   = "Read"
	USER_MESSAGE_STATUS_UNREAD = "UnRead"
)

/* Core config */
const (
	CORE_CONFIG_STATUS_YES       = 1
	CORE_CONFIG_IS_ENCRYPTED_YES = 1
)

/* Core media */
const (
	CORE_MEDIA_MIME_TYPE_IMAGE = "image"
	CORE_MEDIA_MIME_TYPE_VIDEO = "video"
)

/* Core mail */
const (
	CORE_MAIL_STATUS_NO = 0
)

/* Core notification action */
const (
	CORE_NOTIFICATION_ACTION_STATUS_YES = 1
	CORE_NOTIFICATION_ACTION_STATUS_NO  = 0
)

const (
	CORE_NOTIFICATION_ACTION_IS_MAIL_YES     = 1
	CORE_NOTIFICATION_ACTION_IS_SETTING_USER = 1
	CORE_NOTIFICATION_ACTION_IS_FCM_YES      = 1
	CORE_NOTIFICATION_ACTION_IS_TRIGGER_YES  = 1
	CORE_NOTIFICATION_ACTION_IS_MESSAGE_YES  = 1
	CORE_NOTIFICATION_ACTION_IS_SMS_YES      = 1
)

/* Core notification recipients */
const (
	CORE_NOTIFICATION_RECIPIENTS_STATUS_READ   = "Read"
	CORE_NOTIFICATION_RECIPIENTS_STATUS_UNREAD = "UnRead"
)
