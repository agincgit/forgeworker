package config

import (
    "crypto/rand"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"

    log "github.com/sirupsen/logrus"
    "github.com/spf13/viper"
)

// Config holds application settings
type Config struct {
    LogDepth        string
    LogLocation     string
    AutoAppPath     string
    AutoAppConfPath string
    AutoAppLogPath  string
    TaskForgeAPIURL string // TaskForge API URL
    HostName        string // Hostname of the machine
}

// generateWorkerName generates a random worker name if hostname is unavailable.
func generateWorkerName() string {
    b := make([]byte, 4)
    _, _ = rand.Read(b)
    return fmt.Sprintf("worker-%x", b)
}

// GetConfig loads app settings from cmdline or file
func GetConfig(name string) *Config {

    var (
        err                     error
        curDir, confDir, logDir string
        hostName                string
    )

    if curDir, err = os.Getwd(); err != nil {
        log.Fatal(err)
    }
    confDir = filepath.Join(curDir, "appconfig")
    logDir = filepath.Join(curDir, "applogs")

    if _, err := os.Stat(filepath.Join(confDir, name)); os.IsNotExist(err) {
        log.Panicln("Config file not found")
    }
    log.Debug(confDir)
    viper.SetConfigName(name)
    viper.SetConfigType("json")
    viper.AddConfigPath(confDir)
    viper.AddConfigPath(".")

    if err = viper.ReadInConfig(); err != nil {
        log.Debug("Config initialized")
        viper.Set("LogLevel", "Warning")
        viper.Set("LogLocation", logDir)
        viper.Set("TaskForgeAPIURL", "https://api.taskforge.local")

        viper.WriteConfigAs(filepath.Join(confDir, "config.json"))
    } else {
        log.Debug("Config Loaded")
    }

    // Retrieve the hostname of the machine
    hostName, err = os.Hostname()
    if err != nil || hostName == "" {
        // Fallback to command execution if os.Hostname() fails
        out, cmdErr := exec.Command("hostname").Output()
        if cmdErr == nil {
            hostName = strings.TrimSpace(string(out))
        } else {
            log.Warnf("Unable to detect hostname, generating random worker name: %v", err)
            hostName = generateWorkerName()
        }
    }

    return &Config{
        LogDepth:        viper.GetString("LogLevel"),
        LogLocation:     viper.GetString("LogLocation"),
        AutoAppPath:     curDir,
        AutoAppConfPath: confDir,
        AutoAppLogPath:  logDir,
        TaskForgeAPIURL: viper.GetString("TaskForgeAPIURL"),
        HostName:        hostName,
    }
}
