package mail

// The language of the mail Captain sends to users, chosen by the operator
// in Settings → Mail. It is a separate choice from the console's own
// language, which each staff member picks in their browser: the mail goes
// to customers, not to staff.
//
// Unset means English. Every message is short on purpose — a code, a date,
// a percentage and one button — so a translation is a handful of strings
// rather than a template language.

// Language is one option in the picker.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"` // in the language itself
}

// Languages are the options, in the order the picker shows them.
var Languages = []Language{
	{"en", "English"},
	{"zh-CN", "简体中文"},
	{"zh-TW", "繁體中文"},
	{"ja", "日本語"},
	{"ru", "Русский"},
	{"ko", "한국어"},
}

// texts holds one language's strings. Subjects and bodies are format
// strings; the placeholders are noted per field.
type texts struct {
	codeSubject  string // site, code
	codeTitle    string
	codeIntro    string
	resetSubject string // site, code
	resetTitle   string
	resetIntro   string

	expirySubject string // site, date (01-02)
	expiryBody    string // date+time
	expiryButton  string

	trafficSubject string // site, percent
	trafficBody    string // percent
	trafficButton  string

	testSubject string // site
	testBody    string
}

var catalog = map[string]texts{
	"en": {
		codeSubject:  "[%s] Verification code %s",
		codeTitle:    "Verification code",
		codeIntro:    "You are creating an account. Your code is valid for 10 minutes:",
		resetSubject: "[%s] Password reset code %s",
		resetTitle:   "Reset your password",
		resetIntro:   "You are resetting your password. The code is valid for 10 minutes. If this was not you, you can ignore this email:",

		expirySubject: "[%s] Your subscription expires on %s",
		expiryBody:    "Your subscription expires on <b>%s</b>. The nodes stop serving it then, and resume the moment you renew.",
		expiryButton:  "Renew",

		trafficSubject: "[%s] %d%% of your traffic used",
		trafficBody:    "You have used <b>%d%%</b> of this period's traffic. The nodes stop serving when it runs out; you can buy more or wait for the reset.",
		trafficButton:  "View usage",

		testSubject: "[%s] Test email",
		testBody:    "Mail is configured correctly. This is a test.",
	},
	"zh-CN": {
		codeSubject:  "[%s] 验证码 %s",
		codeTitle:    "验证码",
		codeIntro:    "你正在注册账号，验证码如下，10 分钟内有效：",
		resetSubject: "[%s] 重置密码验证码 %s",
		resetTitle:   "重置密码",
		resetIntro:   "你正在重置密码，验证码如下，10 分钟内有效。如果不是你本人操作，忽略这封邮件即可：",

		expirySubject: "[%s] 订阅将于 %s 到期",
		expiryBody:    "你的订阅将在 <b>%s</b> 到期。到期后节点会停止服务，续费后立即恢复。",
		expiryButton:  "前往续费",

		trafficSubject: "[%s] 流量已使用 %d%%",
		trafficBody:    "本周期流量已使用 <b>%d%%</b>。用完后节点会停止服务，可以购买流量或等待重置。",
		trafficButton:  "查看用量",

		testSubject: "[%s] 测试邮件",
		testBody:    "邮件配置正常，这是一封测试邮件。",
	},
	"zh-TW": {
		codeSubject:  "[%s] 驗證碼 %s",
		codeTitle:    "驗證碼",
		codeIntro:    "你正在註冊帳號，驗證碼如下，10 分鐘內有效：",
		resetSubject: "[%s] 重設密碼驗證碼 %s",
		resetTitle:   "重設密碼",
		resetIntro:   "你正在重設密碼，驗證碼如下，10 分鐘內有效。如果不是你本人操作，忽略這封郵件即可：",

		expirySubject: "[%s] 訂閱將於 %s 到期",
		expiryBody:    "你的訂閱將在 <b>%s</b> 到期。到期後節點會停止服務，續約後立即恢復。",
		expiryButton:  "前往續約",

		trafficSubject: "[%s] 流量已使用 %d%%",
		trafficBody:    "本週期流量已使用 <b>%d%%</b>。用完後節點會停止服務，可以購買流量或等待重置。",
		trafficButton:  "查看用量",

		testSubject: "[%s] 測試郵件",
		testBody:    "郵件設定正常，這是一封測試郵件。",
	},
	"ja": {
		codeSubject:  "[%s] 認証コード %s",
		codeTitle:    "認証コード",
		codeIntro:    "アカウント登録の認証コードです。10 分間有効です：",
		resetSubject: "[%s] パスワード再設定コード %s",
		resetTitle:   "パスワードの再設定",
		resetIntro:   "パスワード再設定の認証コードです。10 分間有効です。心当たりがない場合は、このメールは無視してください：",

		expirySubject: "[%s] サブスクリプションは %s に期限切れになります",
		expiryBody:    "サブスクリプションの有効期限は <b>%s</b> です。期限を過ぎるとノードの提供が停止し、更新すればすぐに再開します。",
		expiryButton:  "更新する",

		trafficSubject: "[%s] 通信量を %d%% 使用しました",
		trafficBody:    "今期の通信量を <b>%d%%</b> 使用しました。使い切るとノードの提供が停止します。追加購入するか、リセットをお待ちください。",
		trafficButton:  "使用量を見る",

		testSubject: "[%s] テストメール",
		testBody:    "メール設定は正常です。これはテストメールです。",
	},
	"ru": {
		codeSubject:  "[%s] Код подтверждения %s",
		codeTitle:    "Код подтверждения",
		codeIntro:    "Вы создаёте аккаунт. Код действует 10 минут:",
		resetSubject: "[%s] Код для сброса пароля %s",
		resetTitle:   "Сброс пароля",
		resetIntro:   "Вы сбрасываете пароль. Код действует 10 минут. Если это были не вы, просто проигнорируйте письмо:",

		expirySubject: "[%s] Подписка заканчивается %s",
		expiryBody:    "Ваша подписка заканчивается <b>%s</b>. После этого узлы перестанут её обслуживать и возобновят работу сразу после продления.",
		expiryButton:  "Продлить",

		trafficSubject: "[%s] Использовано %d%% трафика",
		trafficBody:    "Вы использовали <b>%d%%</b> трафика за этот период. Когда он закончится, узлы перестанут обслуживать подписку: можно докупить трафик или дождаться сброса.",
		trafficButton:  "Посмотреть расход",

		testSubject: "[%s] Тестовое письмо",
		testBody:    "Почта настроена правильно. Это тестовое письмо.",
	},
	"ko": {
		codeSubject:  "[%s] 인증 코드 %s",
		codeTitle:    "인증 코드",
		codeIntro:    "계정을 만들기 위한 인증 코드입니다. 10분간 유효합니다:",
		resetSubject: "[%s] 비밀번호 재설정 코드 %s",
		resetTitle:   "비밀번호 재설정",
		resetIntro:   "비밀번호 재설정 인증 코드입니다. 10분간 유효합니다. 본인이 요청하지 않았다면 이 메일은 무시하셔도 됩니다:",

		expirySubject: "[%s] 구독이 %s에 만료됩니다",
		expiryBody:    "구독이 <b>%s</b>에 만료됩니다. 만료되면 노드 이용이 중단되며, 연장하면 즉시 다시 사용할 수 있습니다.",
		expiryButton:  "연장하기",

		trafficSubject: "[%s] 트래픽 %d%% 사용",
		trafficBody:    "이번 주기 트래픽의 <b>%d%%</b>를 사용했습니다. 모두 사용하면 노드 이용이 중단됩니다. 추가로 구매하거나 초기화를 기다리세요.",
		trafficButton:  "사용량 보기",

		testSubject: "[%s] 테스트 메일",
		testBody:    "메일 설정이 정상입니다. 테스트 메일입니다.",
	},
}

// textsFor returns a language's strings, falling back to English for an
// empty or unknown code so a bad setting can never send an empty mail.
func textsFor(lang string) texts {
	if t, ok := catalog[lang]; ok {
		return t
	}
	return catalog["en"]
}
