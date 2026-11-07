package chain

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPackageProxyAllowedOnlyOfficialOrigins(t *testing.T) {
	cases:=[]struct{dest string; allowed bool}{
		{"ppa.launchpadcontent.net:443",true},
		{"ppa.launchpad.net:80",true},
		{"keyserver.ubuntu.com:443",true},
		{"127.0.0.1:443",false},
		{"localhost:443",false},
		{"evil.example.com:443",false},
		{"ppa.launchpadcontent.net.evil.org:443",false},
		{"ppa.launchpadcontent.net:444",false},
		{"[::1]:443",false},
	}
	for _,tc:=range cases {
		_,allowed:=packageProxyDestination(tc.dest)
		if allowed!=tc.allowed {t.Errorf("destination %q allowed=%v want %v",tc.dest,allowed,tc.allowed)}
	}
}
func TestProxyRejectsLocalHTTPRequests(t *testing.T) {
	h:=packageProxyHandler()
	for _,target:=range []string{
		"http://localhost/admin",
		"http://127.0.0.1:51821/",
		"http://ppa.launchpadcontent.net.evil.org/metadata",
	}{
		request:=httptest.NewRequest(http.MethodGet,target,nil)
		response:=httptest.NewRecorder()
		h.ServeHTTP(response,request)
		if response.Code!=http.StatusForbidden {
			t.Errorf("unexpected HTTP result for %s: %d",target,response.Code)
		}
	}
}
func TestProxyRejectsUnsafeMethods(t *testing.T) {
	h:=packageProxyHandler()
	req:=httptest.NewRequest(http.MethodPost,"http://ppa.launchpadcontent.net/test",nil)
	w:=httptest.NewRecorder()
	h.ServeHTTP(w,req)
	if w.Code!=http.StatusMethodNotAllowed {t.Fatalf("status %d",w.Code)}
}
func TestProxyIsUsedOnlyForUnavailablePPA(t *testing.T) {
	for _,msg:=range []string{
		"AWG Chain: PPA недоступен: проверены HTTPS/IPv6",
		"AWG Chain: ключ подписи PPA недоступен по IPv6 и IPv4",
	} {
		if !isPPADownloadFailure(errors.New(msg)) {t.Errorf("should trigger relay: %s",msg)}
	}
	for _,msg:=range []string{"установлен неверный GPG-ключ", "полный отпечаток GPG-ключа не совпадает", "не удалось загрузить модуль", "ошибка SSH"} {
		if isPPADownloadFailure(errors.New(msg)) {t.Errorf("must not retry via relay: %s",msg)}
	}
}
