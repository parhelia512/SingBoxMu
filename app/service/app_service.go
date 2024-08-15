package service

import (
	"changeme/app/model"
	"changeme/app/model/response"
	"changeme/app/utils"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

type AppService struct{}

var singBox *exec.Cmd

// DownloadLatestKernel 下载最新内核
func (g *AppService) DownloadLatestKernel() response.ResInfo {
	// 获取当前系统类型
	goos := runtime.GOOS
	// 获取系统架构
	goarch := runtime.GOARCH

	// 根据系统类型和架构下载对应的内核
	system := fmt.Sprintf("%s-%s.zip", goos, goarch)

	resp, err := http.Get("https://api.github.com/repos/SagerNet/sing-box/releases/latest")
	if err != nil {
		slog.Error("Failed to get latest kernel version", "error", err)
		return response.Error("网络连接失败")
	}
	defer resp.Body.Close()

	var releases model.SingBoxReleases

	err = json.NewDecoder(resp.Body).Decode(&releases)
	if err != nil {
		slog.Error("Failed to decode latest kernel version", "error", err)
		return response.Error("获取最新内核版本失败")
	}

	for _, info := range releases.Assets {
		if strings.Contains(info.Name, system) {
			appUtils := utils.NewAppUtils()
			fileName, _ := appUtils.GetAppDir("sing-box", info.Name)
			err = utils.DownloadFile(info.BrowserDownloadUrl, fileName)
			if err != nil {
				slog.Error("Failed to download latest kernel version", "error", err)
				return response.Error("下载最新内核版本失败")
			}
			// 解压
			exePath, _ := appUtils.GetAppDir("sing-box")

			err = utils.Unzip(fileName, exePath)
			if err != nil {
				slog.Error("Failed to unzip latest kernel version", "error", err)
				return response.Error("解压最新内核版本失败")
			}
			break
		}
	}
	return response.Success("下载最新内核版本成功")
}

// DownloadSubscription 下载订阅
func (g *AppService) DownloadSubscription(url string) response.ResInfo {
	resp, err := http.Get(url)
	if err != nil {
		slog.Error("Failed to get subscription", "error", err)
		return response.Error("下载订阅失败")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read subscription", "error", err)
		return response.Error("解析订阅失败")
	}

	var outInfo model.DownloadSingBoxConfig
	err = json.Unmarshal(body, &outInfo)
	if err != nil {
		slog.Error("Failed to unmarshal subscription", "error", err)
		return response.Error("解析订阅失败")
	}

	// 获取模版
	var inInfo model.SingBoxConfig
	err = json.Unmarshal(model.SingBoxConfigTemplate, &inInfo)
	if err != nil {
		slog.Error("Failed to unmarshal template", "error", err)
		return response.Error("默认配置解析失败")
	}
	var newInfo model.DownloadSingBoxConfig
	for _, item := range outInfo.Outbounds {
		// 只需要url不为空
		if item.Server != "" {
			inInfo.Outbounds = append(inInfo.Outbounds, item)
			newInfo.Outbounds = append(newInfo.Outbounds, item)
		}
	}

	for _, item := range newInfo.Outbounds {
		inInfo.Outbounds[0].Outbounds = append(inInfo.Outbounds[0].Outbounds, item.Tag)
		inInfo.Outbounds[1].Outbounds = append(inInfo.Outbounds[1].Outbounds, item.Tag)
	}

	// 处理订阅
	for _, item := range inInfo.Outbounds {
		if !item.Tls.Enabled {
			item.Tls = model.SingBoxInboundsTls{}
		}
	}

	// json转字符
	data, err := json.Marshal(inInfo)

	appUtils := utils.NewAppUtils()
	config, _ := appUtils.GetAppDir("sing-box", "config.json")

	err = os.WriteFile(config, data, 0644)
	if err != nil {
		slog.Error("Failed to write config", "error", err)
		return response.Error("写入配置失败")
	}
	return response.Success("下载订阅成功")
}

// ChangeProxyMode 设置代理
func (g *AppService) ChangeProxyMode(mode string) response.ResInfo {
	if mode == "system" {
		err := utils.SetProxy()
		if err != nil {
			slog.Error("Failed to set proxy", "error", err)
			return response.Error("Failed to set proxy")
		}
		if singBox != nil && singBox.Process != nil {
			g.RestartCommand()
		}
		return response.Success("设置成功")
	} else if mode == "tun" {
		err := utils.SetTun()
		appUtils := utils.NewAppUtils()
		if err != nil {
			slog.Error("Failed to set tun", "error", err)
			return response.Error("Failed to set tun")
		}
		if singBox != nil && singBox.Process != nil {
			g.RestartCommand()
			// 仅支持windows
			if runtime.GOOS == "windows" {
				appUtils.SetSystemProxy("", false)
			}
		}
		return response.Success("设置成功")
	}
	return response.Error("切换失败，未知的模式")
}

// StartCommand 启动内核
func (g *AppService) StartCommand() response.ResInfo {
	appUtils := utils.NewAppUtils()
	exePath, _ := appUtils.GetAppDir("sing-box", "sing-box")
	wordPath, _ := appUtils.GetAppDir("sing-box")
	cmd := exec.Command(exePath, "run", "-D", wordPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	err := cmd.Start()

	if err != nil {
		fmt.Printf("Error starting command: %v\n", err)
		return response.Error("Error starting command")
	}

	slog.Info("Started command with PID %d\n", cmd.Process.Pid)
	singBox = cmd

	ListenKernelInfo()

	return response.Success("内核已启动")
}

// StopCommand 停止内核
func (g *AppService) StopCommand() response.ResInfo {
	if singBox != nil && singBox.Process != nil {
		// 停止进程
		_ = singBox.Process.Signal(syscall.SIGKILL)
		singBox = nil
		// 清理代理 仅支持windows
		if runtime.GOOS == "windows" {
			utils.NewAppUtils().SetSystemProxy("", false)
		}
		return response.Success("内核已停止")
	}
	return response.Error("内核未启动")
}

// GetVersion 获取内核版本
func (g *AppService) GetVersion() response.ResInfo {
	clash := utils.NewClashClient()
	version, err := clash.GetVersion()
	if err != nil {
		return response.Error("内核未启动")
	}
	return response.Success(version)
}

// ListenKernelInfo 监听日志
func ListenKernelInfo() {
	clash := utils.NewClashClient()
	go clash.GetLogs()
	go clash.GetMemory()
	go clash.GetTraffic()
}

// RestartCommand Function to restart a command
func (g *AppService) RestartCommand() {
	g.StopCommand()
	g.StartCommand()
}

// SetAutoStart 设置开机自启
func (g *AppService) SetAutoStart() response.ResInfo {
	// 获取当前系统类型
	task := utils.NewAppUtils()
	if err := task.RegisterStartup(); err != nil {
		slog.Error("Failed to create task", "error", err)
		return response.Error("Failed to create task")
	}
	return response.Success("设置成功")
}

// RemoveAutoStart 移除开机自启
func (g *AppService) RemoveAutoStart() response.ResInfo {
	// 获取当前系统类型
	task := utils.NewAppUtils()
	if err := task.UnregisterStartup(); err != nil {
		slog.Error("Failed to delete task", "error", err)
		return response.Error("Failed to delete task")
	}
	return response.Success("移除成功")
}

// IsRunningAsAdmin 获取是否是管理员运行
func (g *AppService) IsRunningAsAdmin() response.ResInfo {
	appUtils := utils.NewAppUtils()
	if appUtils.IsRunningAsAdmin() {
		return response.Success("是管理员运行")
	}
	return response.Error("不是管理员运行")
}

// RestartAsAdmin 以管理员重启
func (g *AppService) RestartAsAdmin() response.ResInfo {
	appUtils := utils.NewAppUtils()
	if !appUtils.IsRunningAsAdmin() {
		if singBox != nil && singBox.Process != nil {
			g.StopCommand()
		}
		err := appUtils.RunAsAdmin()
		if err != nil {
			return response.Error("以管理员权限重启失败")
		}
		return response.Success("以管理员权限重启成功")
	}
	return response.Success("已经是管理员运行")
}

// GetProxies 获取所有代理
func (g *AppService) GetProxies() response.ResInfo {
	clash := utils.NewClashClient()
	proxies, err := clash.GetProxies()
	if err != nil {
		return response.Error("获取代理失败")
	}
	return response.Success(proxies)
}

// SwitchProxy 更换代理
func (g *AppService) SwitchProxy(group, name string) response.ResInfo {
	clash := utils.NewClashClient()
	err := clash.SwitchProxy(group, name)
	if err != nil {
		return response.Error("更换代理失败")
	}
	return response.Success("更换代理成功")
}
