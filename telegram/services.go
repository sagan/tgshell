package telegram

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sagan/tgshell/config"
	"github.com/sagan/tgshell/constants"
)

type Service struct {
	backend *url.URL
	headers [][2]string
}

type ServicesProxy struct {
	backends   map[string]*Service
	https      bool
	publicport int
	secret     []byte
	proxy      *httputil.ReverseProxy
}

func (sp *ServicesProxy) UpdateSecret(secret string) {
	h := sha256.New()
	io.WriteString(h, secret)
	sp.secret = h.Sum(nil)
}

// get service url (with auth token), click the url will set the token
// and then redirect to service root
func (sp *ServicesProxy) GetUrl(service string) (string, error) {
	if backend := sp.backends[service]; backend == nil {
		return "", fmt.Errorf("service %s not found", service)
	} else if ss, err := sp.newToken(service, constants.SERVICE_AUTHTOKEN_MAXAGE); err != nil {
		return "", fmt.Errorf("failed to sign url: %v", err)
	} else {
		serviceUrl := ""
		if sp.https {
			serviceUrl += "https://"
		} else {
			serviceUrl += "http://"
		}
		serviceUrl += service
		if sp.https && sp.publicport != 443 || !sp.https && sp.publicport != 80 {
			serviceUrl += fmt.Sprintf(":%d", sp.publicport)
		}
		serviceUrl += constants.SERVICE_AUTH_PREFIX + ss
		return serviceUrl, nil
	}
}

func (sp *ServicesProxy) handleAuthFunc(w http.ResponseWriter, r *http.Request) {
	if hostname := strings.Split(r.Host, ":")[0]; sp.backends[hostname] == nil {
		http.NotFound(w, r)
	} else if cookie, err := r.Cookie(constants.SERVICE_COOKIE_NAME); err == nil &&
		sp.verifyToken(cookie.Value, hostname) == nil {
		// already has valid cookie
		http.Redirect(w, r, "/", http.StatusFound)
	} else if i := strings.LastIndex(r.URL.Path, "/"); i == -1 {
		http.Error(w, "Bad request", http.StatusBadRequest)
	} else if tokenString := r.URL.Path[i+1:]; tokenString == "" {
		http.Error(w, "Bad request", http.StatusBadRequest)
	} else if err := sp.verifyToken(tokenString, hostname); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
	} else if token, err := sp.newToken(hostname, constants.SERVICE_COOKIE_MAXAGE); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create permanent token: %v", err), http.StatusInternalServerError)
	} else {
		http.SetCookie(w, &http.Cookie{
			Name:     constants.SERVICE_COOKIE_NAME,
			Path:     "/",
			Value:    token,
			MaxAge:   constants.SERVICE_COOKIE_MAXAGE,
			HttpOnly: true,
		})
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (sp *ServicesProxy) newToken(subject string, maxAge int) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Second * time.Duration(maxAge))),
	})
	return token.SignedString(sp.secret)
}

func (sp *ServicesProxy) verifyToken(tokenString string, hostname string) error {
	if token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return sp.secret, nil
	}); err != nil || token.Claims == nil {
		return fmt.Errorf("invalid auth token: %v", err)
	} else if subject, err := token.Claims.GetSubject(); err != nil {
		return fmt.Errorf("invalid auth token: no subject: %v", err)
	} else if subject != hostname {
		return fmt.Errorf("invalid token subject %s", subject)
	} else if expireTime, err := token.Claims.GetExpirationTime(); err != nil {
		return fmt.Errorf("invalid token expiration time: %v", err)
	} else if expireTime.Time.Unix() <= time.Now().Unix() {
		return fmt.Errorf("token has expired")
	}
	return nil
}

func (sp *ServicesProxy) handleFunc(w http.ResponseWriter, r *http.Request) {
	if hostname := strings.Split(r.Host, ":")[0]; sp.backends[hostname] == nil {
		http.NotFound(w, r)
	} else if cookie, err := r.Cookie(constants.SERVICE_COOKIE_NAME); err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	} else if err := sp.verifyToken(cookie.Value, hostname); err != nil {
		http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
	} else {
		sp.proxy.ServeHTTP(w, r)
	}
}

func (sp *ServicesProxy) proxyRewriteFunc(r *httputil.ProxyRequest) {
	service := sp.backends[strings.Split(r.In.Host, ":")[0]]
	r.Out.URL.Scheme = service.backend.Scheme
	r.Out.URL.Host = service.backend.Host
	r.Out.URL.Path, r.Out.URL.RawPath = r.In.URL.Path, r.In.URL.RawPath
	r.Out.URL.RawQuery = r.In.URL.RawQuery
	r.Out.Header.Set("X-Forwarded-Host", r.In.Host)
	r.Out.Header.Set("X-Forwarded-Proto", r.In.URL.Scheme)
	for _, header := range service.headers {
		if header[1] != "" {
			r.Out.Header.Set(header[0], header[1])
		} else {
			r.Out.Header.Del(header[0])
		}
	}
}

// Service flow:
// 1. user send /services in tg. 2. bot generate token and send http://<public_url>/__auth__/<token> to user.
// 3. User click the link which has tokens, reverse proxy set "Set-Cookie" header, then redirecting to /.
// 4. Reverse proxy check every incoming request for valid cookie before forwarding to backend.
// <token> : one-time use token for generating cookie, will expire soon.
// <cookie> : long-time valid token.
func NewServicesProxy(serviceConfigs []*config.ConfigServiceStruct, addr string,
	port int, publicport int, https bool, secret string) (
	*ServicesProxy, error) {
	if publicport == 0 {
		publicport = port
	}
	sp := &ServicesProxy{https: https, publicport: publicport}
	if len(serviceConfigs) == 0 {
		return sp, nil
	}

	backends := map[string]*Service{}
	for _, serviceConfig := range serviceConfigs {
		if serviceConfig.Hostname == "" {
			log.Fatalf("service hostname can NOT be empty")
		}
		if backends[serviceConfig.Hostname] != nil {
			log.Fatalf("duplicate service hostname found")
		}
		urlObj, err := url.Parse(serviceConfig.Backend)
		if err != nil {
			log.Fatalf("Failed to parse service backend %s: %v", serviceConfig.Backend, err)
		}
		log.Printf("proxy %s => %s", serviceConfig.Hostname, urlObj.String())
		backends[serviceConfig.Hostname] = &Service{
			backend: urlObj,
			headers: serviceConfig.Headers,
		}
	}
	sp.backends = backends

	sp.UpdateSecret(secret)
	sp.proxy = &httputil.ReverseProxy{
		Rewrite: sp.proxyRewriteFunc,
	}
	http.HandleFunc("/", sp.handleFunc)
	http.HandleFunc(constants.SERVICE_AUTH_PREFIX, sp.handleAuthFunc)
	log.Printf("Services listening on %s:%d. Public port=%d, https=%t", addr, port, publicport, https)
	go func() {
		log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%d", addr, port), nil))
	}()
	return sp, nil
}
