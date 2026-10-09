// Command license-tool 是维护者本地使用的授权签发工具。
//
// ⚠️ 这个工具**只在你自己的电脑上运行**，绝不要编译进交付给客户的程序，
// 也不要把私钥文件放进代码仓库或随程序打包。
// 客户手里只有公钥（已编译进主程序），无法反推私钥，因此无法伪造授权。
//
// 用法示例：
//
//	生成密钥对（只需做一次，之后一直复用同一对密钥）
//	  go run ./cmd/license-tool genkey
//
//	签发一份有效期一年的授权
//	  go run ./cmd/license-tool issue -key private.key -customer "某某公司" -days 365 -out license.lic
//
//	签发一份指定到期日期的授权
//	  go run ./cmd/license-tool issue -key private.key -customer "某某公司" -expire 2027-06-30 -out license.lic
//
//	查看/验证一份授权文件（不需要私钥，用公钥即可）
//	  go run ./cmd/license-tool verify -pub public.key -lic license.lic
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/pkg/license"
)

const usage = `license-tool —— webb-api 离线授权签发工具（仅维护者本地使用）

子命令：
  genkey    生成 RSA 密钥对（private.key / public.key）
  issue     签发授权文件 license.lic
  verify    用公钥查看并验证一份授权文件
  decode    仅解码授权内容，不做签名校验（排查用）

示例：
  go run ./cmd/license-tool genkey
  go run ./cmd/license-tool issue -key private.key -customer "某某公司" -days 365 -out license.lic
  go run ./cmd/license-tool issue -key private.key -expire 2027-06-30 -out license.lic
  go run ./cmd/license-tool verify -pub public.key -lic license.lic

安全提醒：
  private.key 只保存在你自己电脑上，丢失后无法为已有客户续签（需重新 genkey 并重新编译程序）。
  切勿提交到 Git、切勿发给客户、切勿打进交付包。
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "genkey":
		err = runGenKey(os.Args[2:])
	case "issue":
		err = runIssue(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "decode":
		err = runDecode(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Printf("未知子命令: %s\n\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

// runGenKey 生成一对 RSA 密钥。
//
// 生成后需要把 public.key 的内容粘贴到 pkg/license/license.go 的 publicKeyPEM 常量中，
// 然后重新编译主程序。这一步是整个授权体系的根基，只需做一次。
func runGenKey(args []string) error {
	fs := flag.NewFlagSet("genkey", flag.ExitOnError)
	bits := fs.Int("bits", 2048, "RSA 密钥长度（位）")
	outDir := fs.String("out", ".", "密钥输出目录")
	force := fs.Bool("force", false, "允许覆盖已存在的密钥文件")
	_ = fs.Parse(args)

	if *bits < 2048 {
		return errors.New("密钥长度不得小于 2048 位，否则签名强度不足")
	}

	privPath := joinPath(*outDir, "private.key")
	pubPath := joinPath(*outDir, "public.key")

	if !*force {
		for _, p := range []string{privPath, pubPath} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("%s 已存在，直接覆盖会导致此前签发的全部授权失效；如确需重新生成请加 -force", p)
			}
		}
	}

	fmt.Printf("正在生成 %d 位 RSA 密钥对...\n", *bits)
	priv, err := rsa.GenerateKey(rand.Reader, *bits)
	if err != nil {
		return fmt.Errorf("生成密钥失败: %w", err)
	}

	privPEM := license.EncodePrivateKeyPEM(priv)
	pubPEM, err := license.EncodePublicKeyPEM(&priv.PublicKey)
	if err != nil {
		return err
	}

	// 私钥用 0600，只有文件所有者可读写。
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return fmt.Errorf("写入私钥失败: %w", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return fmt.Errorf("写入公钥失败: %w", err)
	}

	fmt.Printf("\n✅ 密钥对已生成：\n")
	fmt.Printf("   私钥（自己保管，绝不外传）: %s\n", privPath)
	fmt.Printf("   公钥（粘贴进源码）      : %s\n\n", pubPath)
	fmt.Println("下一步：把下面这段公钥完整复制，替换 pkg/license/license.go 中 publicKeyPEM 常量的内容，然后重新编译主程序。")
	fmt.Println()
	fmt.Println(strings.TrimSpace(string(pubPEM)))
	fmt.Println("\n⚠️ 私钥一旦丢失，已签发给客户的所有授权都无法续签，只能重新生成密钥并重新交付程序。请立即妥善备份。")
	return nil
}

// runIssue 用私钥签发一份授权文件。
//
// 这是日常最常用的命令：客户续费时，改一下到期日期重新签一份发过去即可。
func runIssue(args []string) error {
	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	keyPath := fs.String("key", "private.key", "私钥文件路径")
	customer := fs.String("customer", "", "客户标识（如公司名），仅用于你自己追溯，不影响校验")
	product := fs.String("product", "webb-api", "产品名")
	days := fs.Int("days", 0, "从当前时间起的有效天数，与 -expire 二选一")
	expire := fs.String("expire", "", "到期日期，格式 2006-01-02 或 2006-01-02T15:04，按本地时区解析")
	grace := fs.Int("grace", license.DefaultGraceDays(), "到期后的宽限天数，宽限期内仍可用但会提示续期")
	out := fs.String("out", "license.lic", "输出的授权文件路径")
	show := fs.Bool("show", false, "同时把授权内容打印到终端")
	_ = fs.Parse(args)

	priv, err := loadPrivateKey(*keyPath)
	if err != nil {
		return err
	}

	expireTime, err := resolveExpireTime(*days, *expire)
	if err != nil {
		return err
	}
	if *grace < 0 {
		return errors.New("宽限天数不能为负数")
	}

	data := license.Data{
		Product:    *product,
		Customer:   *customer,
		IssuedAt:   time.Now().UTC(),
		ExpireTime: expireTime,
		GraceDays:  *grace,
	}

	content, err := license.Issue(priv, data)
	if err != nil {
		return err
	}

	if err := os.WriteFile(*out, content, 0o644); err != nil {
		return fmt.Errorf("写入授权文件失败: %w", err)
	}

	fmt.Printf("✅ 授权文件已生成: %s\n\n", *out)
	fmt.Printf("   产品      : %s\n", data.Product)
	if *customer != "" {
		fmt.Printf("   客户      : %s\n", *customer)
	}
	fmt.Printf("   签发时间  : %s\n", data.IssuedAt.Local().Format("2006-01-02 15:04"))
	fmt.Printf("   到期时间  : %s（本地时区）\n", expireTime.Local().Format("2006-01-02 15:04"))
	fmt.Printf("   宽限期    : %d 天（到期后仍可继续使用，超过则彻底停用）\n", *grace)
	fmt.Printf("   到期时刻  : %s\n", expireTime.UTC().Format(time.RFC3339))

	if *show {
		fmt.Printf("\n--- 文件内容 ---\n%s\n", string(content))
	}

	fmt.Print("\n把这个文件发给客户，让客户在管理后台「授权管理」页面上传即可生效，无需重启服务。\n")
	return nil
}

// runVerify 用公钥验证一份授权文件，并展示其内容。
// 客户反馈"授权用不了"时，先用这个命令自查，能快速区分是签名问题还是到期问题。
func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pubPath := fs.String("pub", "public.key", "公钥文件路径")
	licPath := fs.String("lic", "license.lic", "待验证的授权文件路径")
	_ = fs.Parse(args)

	pub, err := loadPublicKey(*pubPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(*licPath)
	if err != nil {
		return fmt.Errorf("读取授权文件失败: %w", err)
	}

	data, signErr := verifyLicenseContent(pub, content)

	fmt.Printf("授权文件: %s\n", *licPath)
	if signErr != nil {
		fmt.Printf("\n❌ 验证失败: %v\n", signErr)
		return signErr
	}

	fmt.Printf("\n✅ 签名校验通过，内容未被篡改\n\n")
	fmt.Printf("   产品      : %s\n", data.Product)
	fmt.Printf("   客户      : %s\n", orNone(data.Customer))
	fmt.Printf("   签发时间  : %s\n", data.IssuedAt.Local().Format("2006-01-02 15:04"))
	fmt.Printf("   到期时间  : %s\n", data.ExpireTime.Local().Format("2006-01-02 15:04"))
	fmt.Printf("   宽限期    : %d 天\n", data.GraceDays)

	now := time.Now().UTC()
	if now.After(data.ExpireTime) {
		over := now.Sub(data.ExpireTime)
		grace := time.Duration(data.GraceDays) * 24 * time.Hour
		if data.GraceDays > 0 && over <= grace {
			fmt.Printf("\n⚠️ 已过期 %s，但仍在 %d 天宽限期内，系统可继续使用（会提示续期）\n",
				roundDuration(over), data.GraceDays)
		} else {
			fmt.Printf("\n❌ 已过期 %s，宽限期已结束，系统将停用\n", roundDuration(over))
		}
	} else {
		remaining := data.ExpireTime.Sub(now)
		fmt.Printf("\n✅ 当前有效，剩余 %d 天（%s）\n", int(remaining.Hours()/24), roundDuration(remaining))
	}
	return nil
}

// runDecode 只解码不验签，用于排查"客户发来的文件到底写了什么"。
// 注意：此命令的输出**不可信**，因为任何人都能构造出能被解码的内容。
func runDecode(args []string) error {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	licPath := fs.String("lic", "license.lic", "待解码的授权文件路径")
	_ = fs.Parse(args)

	content, err := os.ReadFile(*licPath)
	if err != nil {
		return fmt.Errorf("读取授权文件失败: %w", err)
	}

	var f license.File
	if err := json.Unmarshal(content, &f); err != nil {
		return fmt.Errorf("文件不是合法的 JSON 结构: %w", err)
	}

	dataJSON, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		return fmt.Errorf("data 字段无法 base64 解码: %w", err)
	}

	var pretty map[string]any
	if err := json.Unmarshal(dataJSON, &pretty); err != nil {
		fmt.Printf("data 字段解码结果（非 JSON）:\n%s\n", string(dataJSON))
		return nil
	}
	out, _ := json.MarshalIndent(pretty, "", "  ")

	fmt.Printf("⚠️ 以下内容为直接解码所得，**未经签名校验，不可信**：\n\n%s\n", string(out))
	return nil
}

// resolveExpireTime 解析到期时间，支持"有效天数"和"具体日期"两种写法。
//
// 按本地时区解析日期，再转 UTC 存入授权：
// 你在东八区写 2027-06-30，客户服务器无论在哪个时区，
// 判定都用同一个绝对时刻，不会出现"差一天"的争议。
func resolveExpireTime(days int, expire string) (time.Time, error) {
	hasDays := days > 0
	hasExpire := strings.TrimSpace(expire) != ""

	switch {
	case hasDays && hasExpire:
		return time.Time{}, errors.New("-days 与 -expire 只能二选一，不能同时指定")
	case hasDays:
		return time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Truncate(time.Second), nil
	case hasExpire:
		return parseExpireDate(strings.TrimSpace(expire))
	default:
		return time.Time{}, errors.New("必须指定 -days（有效天数）或 -expire（到期日期）其中之一")
	}
}

// parseExpireDate 支持多种常见日期写法，尽量容忍手工输入的不规范。
func parseExpireDate(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02",
		"2006/1/2",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			if t.Before(time.Now()) {
				return time.Time{}, fmt.Errorf("到期时间 %s 已经过去了，请确认日期", t.Format("2006-01-02 15:04"))
			}
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析到期日期 %q，请使用 2006-01-02 或 2006-01-02T15:04 格式", s)
}

// verifyLicenseContent 复用主程序完全相同的校验逻辑，
// 确保"工具认为有效"和"主程序认为有效"的判断标准一致，不会出现两边说法不同。
func verifyLicenseContent(pub *rsa.PublicKey, content []byte) (*license.Data, error) {
	var f license.File
	if err := json.Unmarshal(content, &f); err != nil {
		return nil, errors.New("授权文件格式错误，不是合法的 JSON")
	}
	dataJSON, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		return nil, errors.New("授权内容无法 base64 解码")
	}
	sign, err := base64.StdEncoding.DecodeString(f.Sign)
	if err != nil {
		return nil, errors.New("授权签名无法 base64 解码")
	}
	if err := license.VerifySignature(pub, dataJSON, sign); err != nil {
		return nil, errors.New("签名校验失败：文件已被篡改，或不是用配对的私钥签发的")
	}
	var data license.Data
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		return nil, errors.New("授权数据无法解析")
	}
	return &data, nil
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("找不到私钥文件 %s，请先执行 genkey 生成密钥对", path)
		}
		return nil, fmt.Errorf("读取私钥失败: %w", err)
	}
	key, err := license.ParsePrivateKeyPEM(string(raw))
	if err != nil {
		return nil, fmt.Errorf("私钥解析失败: %w", err)
	}
	return key, nil
}

func loadPublicKey(path string) (*rsa.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("找不到公钥文件 %s", path)
		}
		return nil, fmt.Errorf("读取公钥失败: %w", err)
	}
	key, err := license.ParsePublicKeyPEM(string(raw))
	if err != nil {
		return nil, fmt.Errorf("公钥解析失败: %w", err)
	}
	return key, nil
}

func joinPath(dir, name string) string {
	if strings.TrimSpace(dir) == "" || dir == "." {
		return name
	}
	return strings.TrimSuffix(dir, "/") + string(os.PathSeparator) + name
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（未填写）"
	}
	return s
}

func roundDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		return fmt.Sprintf("%d 天", int(d.Hours()/24))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%d 小时", int(d.Hours()))
	}
	return fmt.Sprintf("%d 分钟", int(d.Minutes()))
}
