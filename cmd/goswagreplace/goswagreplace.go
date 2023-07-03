package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Config struct {
	Replacements []Replacement `json:"replacements"`
}

type Replacement struct {
	FromOperationId string `json:"from_operation_id"`
	ToOperationId   string `json:"to_operation_id"`
	ToPackageAlias  string `json:"to_package_alias"`
	ToPackage       string `json:"to_package"`
}

func main() {
	config := flag.String("config", "config.json", "replacements configuration")
	target := flag.String("target", "./", "the base directory for generating the files")
	serverPackage := flag.String("server-package", "restapi", "the package to save the server specific code")
	appName := flag.String("name", "", "the name of the application, defaults to a mangled value of info.title")
	apiPackage := flag.String("api-package", "operations", "the package to save the operations")
	goFmtCmd := flag.String("gofmt-cmd", "gofmt", "gofmt executable location")
	goFmtArg := flag.String("gofmt-arg", "-w", "gofmt arguments")

	flag.Parse()

	if *appName == "" {
		fmt.Println("--name flag is required")

		return
	}

	apiFileName := filepath.Join(*target, *serverPackage, *apiPackage, *appName+"_api.go")
	apiTmpFileName := apiFileName + ".tmp"

	configureFileName := filepath.Join(*target, *serverPackage, "configure_"+*appName+".go")
	configureTmpFileName := filepath.Join(*target, *serverPackage, "configure_"+*appName+".go.tmp")

	configFile, err := os.OpenFile(*config, os.O_RDONLY, 0666)
	if err != nil {
		fmt.Println(fmt.Sprintf("open config file failed: %s", err))

		return
	}
	defer configFile.Close()

	apiFile, err := os.OpenFile(apiFileName, os.O_RDONLY, 0666)
	if err != nil {
		fmt.Println(fmt.Sprintf("open api file failed: %s", err))

		return
	}
	defer apiFile.Close()

	apiTmpFile, err := os.Create(apiTmpFileName)
	if err != nil {
		fmt.Println(fmt.Sprintf("open api temp file failed: %s", err))

		return
	}
	defer apiTmpFile.Close()

	configureFile, err := os.OpenFile(configureFileName, os.O_RDONLY, 0666)
	if err != nil {
		fmt.Println(fmt.Sprintf("open configure file failed: %s", err))

		return
	}
	defer configFile.Close()

	configureTmpFile, err := os.Create(configureTmpFileName)
	if err != nil {
		fmt.Println(fmt.Sprintf("open configure temp file failed: %s", err))

		return
	}
	defer  configureTmpFile.Close()

	conf, err := loadConfig(configFile)
	if err != nil {
		fmt.Println(fmt.Sprintf("load configure file failed: %s", err))

		return
	}

	if err := rewriteApi(apiFile, apiTmpFile, conf); err != nil {
		fmt.Println(fmt.Sprintf("rewrite api file failed: %s", err))

		return
	}

	if err := rewriteConfigure(configureFile, configureTmpFile, conf, *apiPackage); err != nil {
		fmt.Println(fmt.Sprintf("rewrite configure file failed: %s", err))

		return
	}

	fmtCmd := exec.Command(*goFmtCmd, *goFmtArg, apiTmpFileName, configureTmpFileName)
	fmtCmd.Stdout = os.Stdout
	fmtCmd.Stderr = os.Stderr
	if err := fmtCmd.Run(); err != nil {
		fmt.Println(fmt.Sprintf("gofmt command failed: %s", err))

		return
	}

	if err := os.Rename(apiTmpFileName, apiFileName); err != nil {
		fmt.Println(fmt.Sprintf("update api file failed: %s", err))

		return
	}

	if err := os.Rename(configureTmpFileName, configureFileName); err != nil {
		fmt.Println(fmt.Sprintf("update configure file failed: %s", err))

		return
	}

	fmt.Println("===== REWRITE SUCCESSFUL =====")
	fmt.Println(apiFileName)
	fmt.Println(configureFileName)
}

func loadConfig(f *os.File) (Config, error) {
	c := Config{}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return c, err
	}

	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}

	return c, nil
}

func rewriteApi(in, out *os.File, c Config) error {
	s := bufio.NewScanner(in)
	o := &bytes.Buffer{}

	for s.Scan() {
		str := s.Text()
		if strings.TrimSpace(str) == "import (" {
			o.WriteString(str)
			o.WriteByte('\n')

			for _, i := range c.Replacements {
				o.WriteString(fmt.Sprintf("\t%s \"%s\"\n", i.ToPackageAlias, i.ToPackage))
			}
		} else {
			for _, i := range c.Replacements {
				// in: V2OnboardingHandler V2OnboardingHandler
				// out: V2OnboardingHandler onboarding.OnboardingHandler
				if strings.TrimSpace(str) == fmt.Sprintf("%sHandler %sHandler", i.FromOperationId, i.FromOperationId) {
					str = fmt.Sprintf("\t%sHandler %s.%sHandler", i.FromOperationId, i.ToPackageAlias, i.ToOperationId)
				}

				// in: V2OnboardingHandler: V2OnboardingHandlerFunc(func(params V2OnboardingParams) middleware.Responder {
				// out: V2OnboardingHandler: onboarding.OnboardingHandlerFunc(func(params onboarding.OnboardingParams) middleware.Responder {
				if strings.TrimSpace(str) == fmt.Sprintf("%sHandler: %sHandlerFunc(func(params %sParams) middleware.Responder {", i.FromOperationId, i.FromOperationId, i.FromOperationId) {
					str = fmt.Sprintf("\t\t%sHandler: %s.%sHandlerFunc(func(params %s.%sParams) middleware.Responder {", i.FromOperationId, i.ToPackageAlias, i.ToOperationId, i.ToPackageAlias, i.ToOperationId)
				}

				// in: o.handlers["POST"]["/api/v2/onboarding"] = NewV2Onboarding(o.context, o.V2OnboardingHandler)
				// out: o.handlers["POST"]["/api/v2/onboarding"] = onboarding.NewOnboarding(o.context, o.V2OnboardingHandler)
				if strings.Contains(str, fmt.Sprintf("New%s(o.context, o.%sHandler)", i.FromOperationId, i.FromOperationId)) {
					from := fmt.Sprintf("New%s(o.context, o.%sHandler)", i.FromOperationId, i.FromOperationId)
					to := fmt.Sprintf("%s.New%s(o.context, o.%sHandler)", i.ToPackageAlias, i.ToOperationId, i.FromOperationId)
					str = strings.ReplaceAll(str, from, to)
				}
			}

			o.WriteString(str)
			o.WriteByte('\n')
		}

	}

	if s.Err() != nil {
		return fmt.Errorf("read file failed: %s", s.Err())
	}

	if _, err := o.WriteTo(out); err != nil {
		return fmt.Errorf("write file failed: %s", s.Err())
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("write file failed: %s", s.Err())
	}

	return nil
}

func rewriteConfigure(in, out *os.File, c Config, apiPackage string) error {
	s := bufio.NewScanner(in)
	o := &bytes.Buffer{}

	for s.Scan() {
		str := s.Text()
		if strings.TrimSpace(str) == "import (" {
			o.WriteString(str)
			o.WriteByte('\n')

			for _, i := range c.Replacements {
				o.WriteString(fmt.Sprintf("\t%s \"%s\"\n", i.ToPackageAlias, i.ToPackage))
			}
		} else {
			for _, i := range c.Replacements {
				// in: api.V2OnboardingHandler = operations.V2OnboardingHandlerFunc(func(params operations.V2OnboardingParams) middleware.Responder {
				// out api.V2OnboardingHandler = onboarding.OnboardingHandlerFunc(func(params onboarding.OnboardingParams) middleware.Responder {
				if strings.Contains(str, fmt.Sprintf("api.%sHandler = %s.%sHandlerFunc", i.FromOperationId, apiPackage, i.FromOperationId)) {
					from := fmt.Sprintf("%s.%s", apiPackage, i.FromOperationId)
					to := fmt.Sprintf("%s.%s", i.ToPackageAlias, i.ToOperationId)
					str = strings.ReplaceAll(str, from, to)
				}
			}

			o.WriteString(str)
			o.WriteByte('\n')
		}

	}

	if s.Err() != nil {
		return fmt.Errorf("read file failed: %s", s.Err())
	}

	if _, err := o.WriteTo(out); err != nil {
		return fmt.Errorf("write file failed: %s", s.Err())
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("write file failed: %s", s.Err())
	}

	return nil
}
