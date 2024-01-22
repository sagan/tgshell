package constants

const MAX_HISTORY = 50
const TG_ROW_BUTTONS = 5
const TG_FILES_MAX = 99        // if >= 100, also need to set sprintf wide formatter to %-3s
const TG_TEXT_LIMIT = 4096     // tg accepts message of max 4096 UTF-8 characters
const PTY_H = 100              // PTY height. Some applications refuse to work if width or height is 0
const PTY_W = 100              // PTY width.
const TIMEOUT_MESSAGE = 60 * 3 // Seconds. Ignore tg message which arrives too late
const DEFAULT_SERVICES_PORT = 8085
const DEFAULT_SERVICES_ADDR = "0.0.0.0"
const SERVICE_AUTH_PREFIX = "/__auth__/"
const SERVICE_COOKIE_NAME = "_ts_token"
const SERVICE_AUTHTOKEN_MAXAGE = 1800
const SERVICE_COOKIE_MAXAGE = 86400 * 400
