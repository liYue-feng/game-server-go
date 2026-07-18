package login

import "testing"

func TestDevelopmentCodeExchangerAcceptsConfiguredIdentity(t *testing.T) {
	identity, err := NewDevelopmentCodeExchanger(true).Exchange("dev:editor-001")
	if err != nil || identity.OpenID != "dev:editor-001" {
		t.Fatalf("Exchange() = %#v, %v", identity, err)
	}
}

func TestDevelopmentCodeExchangerRejectsDisabledOrMalformedCode(t *testing.T) {
	for _, tt := range []struct {
		enabled bool
		code    string
	}{
		{false, "dev:editor-001"},
		{true, "wechat-code"},
		{true, "dev:"},
	} {
		if _, err := NewDevelopmentCodeExchanger(tt.enabled).Exchange(tt.code); err == nil {
			t.Fatalf("Exchange(%q) returned nil error", tt.code)
		}
	}
}

func TestWechatCodeExchangerRejectsDevelopmentCodeBeforeClientUse(t *testing.T) {
	if _, err := NewWechatCodeExchanger(nil).Exchange("dev:editor-001"); err == nil {
		t.Fatal("production exchanger accepted a development code")
	}
}
