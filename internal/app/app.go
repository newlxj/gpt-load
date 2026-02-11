// Package app provides the main application logic and lifecycle management.
package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"aimanager/internal/config"
	db "aimanager/internal/db/migrations"
	"aimanager/internal/handler"
	"aimanager/internal/i18n"
	"aimanager/internal/keypool"
	"aimanager/internal/models"
	"aimanager/internal/proxy"
	"aimanager/internal/router"
	"aimanager/internal/services"
	"aimanager/internal/store"
	"aimanager/internal/types"
	"aimanager/internal/version"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

// App holds all services and manages the application lifecycle.
type App struct {
	engine            *gin.Engine
	proxyEngine       *gin.Engine // Separate router for proxy-only port
	configManager     types.ConfigManager
	settingsManager   *config.SystemSettingsManager
	groupManager      *services.GroupManager
	logCleanupService *services.LogCleanupService
	requestLogService *services.RequestLogService
	cronChecker       *keypool.CronChecker
	keyPoolProvider   *keypool.KeyProvider
	proxyServer       *proxy.ProxyServer
	serverHandler     *handler.Server
	storage           store.Store
	db                *gorm.DB
	httpServer        *http.Server
	proxyHTTPServer   *http.Server // Proxy-only server
}

// AppParams defines the dependencies for the App.
type AppParams struct {
	dig.In
	Engine            *gin.Engine
	ConfigManager     types.ConfigManager
	SettingsManager   *config.SystemSettingsManager
	GroupManager      *services.GroupManager
	LogCleanupService *services.LogCleanupService
	RequestLogService *services.RequestLogService
	CronChecker       *keypool.CronChecker
	KeyPoolProvider   *keypool.KeyProvider
	ProxyServer       *proxy.ProxyServer
	ServerHandler     *handler.Server
	Storage           store.Store
	DB                *gorm.DB
}

// NewApp is the constructor for App, with dependencies injected by dig.
func NewApp(params AppParams) *App {
	return &App{
		engine:            params.Engine,
		configManager:     params.ConfigManager,
		settingsManager:   params.SettingsManager,
		groupManager:      params.GroupManager,
		logCleanupService: params.LogCleanupService,
		requestLogService: params.RequestLogService,
		cronChecker:       params.CronChecker,
		keyPoolProvider:   params.KeyPoolProvider,
		proxyServer:       params.ProxyServer,
		serverHandler:     params.ServerHandler,
		storage:           params.Storage,
		db:                params.DB,
	}
}

// Start runs the application, it is a non-blocking call.
func (a *App) Start() error {
	// 初始化 i18n
	if err := i18n.Init(); err != nil {
		return fmt.Errorf("failed to initialize i18n: %w", err)
	}
	logrus.Info("i18n initialized successfully.")

	// Master 节点执行初始化
	if a.configManager.IsMaster() {
		logrus.Info("Starting as Master Node.")

		if err := a.storage.Clear(); err != nil {
			return fmt.Errorf("cache cleanup failed: %w", err)
		}

		// 数据库迁移
		db.HandleLegacyIndexes(a.db)
		if err := a.db.AutoMigrate(
			&models.SystemSetting{},
			&models.Group{},
			&models.GroupSubGroup{},
			&models.APIKey{},
			&models.RequestLog{},
			&models.GroupHourlyStat{},
			&models.GroupMonthlyStat{},
		); err != nil {
			return fmt.Errorf("database auto-migration failed: %w", err)
		}
		// 数据修复
		if err := db.MigrateDatabase(a.db); err != nil {
			return fmt.Errorf("database data migration failed: %w", err)
		}
		logrus.Info("Database auto-migration completed.")

		// 初始化系统设置
		if err := a.settingsManager.EnsureSettingsInitialized(a.configManager.GetAuthConfig()); err != nil {
			return fmt.Errorf("failed to initialize system settings: %w", err)
		}
		logrus.Info("System settings initialized in DB.")

		a.settingsManager.Initialize(a.storage, a.groupManager, a.configManager.IsMaster())

		// 从数据库加载密钥到 Redis
		if err := a.keyPoolProvider.LoadKeysFromDB(); err != nil {
			return fmt.Errorf("failed to load keys into key pool: %w", err)
		}
		logrus.Debug("API keys loaded into Redis cache by master.")

		// 仅 Master 节点启动的服务
		a.requestLogService.Start()
		a.logCleanupService.Start()
		a.cronChecker.Start()
	} else {
		logrus.Info("Starting as Slave Node.")
		a.settingsManager.Initialize(a.storage, a.groupManager, a.configManager.IsMaster())
	}

	// 显示配置并启动所有后台服务
	a.configManager.DisplayServerConfig()

	a.groupManager.Initialize()

	// Create main HTTP server (full access)
	serverConfig := a.configManager.GetEffectiveServerConfig()
	a.httpServer = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.Port),
		Handler:        a.engine,
		ReadTimeout:    time.Duration(serverConfig.ReadTimeout) * time.Second,
		WriteTimeout:   time.Duration(serverConfig.WriteTimeout) * time.Second,
		IdleTimeout:    time.Duration(serverConfig.IdleTimeout) * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Start main HTTP server in a new goroutine
	go func() {
		logrus.Infof("aimanager proxy server started successfully on Version: %s", version.Version)
		logrus.Infof("Internal server (full access): http://%s:%d", serverConfig.Host, serverConfig.Port)
		if serverConfig.ProxyPort > 0 {
			logrus.Infof("External proxy-only port: http://%s:%d", serverConfig.Host, serverConfig.ProxyPort)
		}
		logrus.Info("")
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("Server startup failed: %v", err)
		}
	}()

	// Create proxy-only HTTP server if PROXY_PORT is configured
	if serverConfig.ProxyPort > 0 {
		// Create proxy-only router
		a.proxyEngine = router.NewProxyRouter(
			a.proxyServer,
			a.groupManager,
			a.serverHandler,
			a.configManager,
		)

		a.proxyHTTPServer = &http.Server{
			Addr:           fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.ProxyPort),
			Handler:        a.proxyEngine,
			ReadTimeout:    time.Duration(serverConfig.ReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(serverConfig.WriteTimeout) * time.Second,
			IdleTimeout:    time.Duration(serverConfig.IdleTimeout) * time.Second,
			MaxHeaderBytes: 1 << 20,
		}

		// Start proxy-only HTTP server in a new goroutine
		go func() {
			if err := a.proxyHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logrus.Fatalf("Proxy-only server startup failed: %v", err)
			}
		}()
	}

	return nil
}

// Stop gracefully shuts down the application.
func (a *App) Stop(ctx context.Context) {
	logrus.Info("Shutting down server...")

	serverConfig := a.configManager.GetEffectiveServerConfig()
	totalTimeout := time.Duration(serverConfig.GracefulShutdownTimeout) * time.Second

	// 动态计算 HTTP 关机超时时间，为后台服务固定预留 5 秒
	httpShutdownTimeout := totalTimeout - 5*time.Second
	httpShutdownCtx, cancelHttpShutdown := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancelHttpShutdown()

	// Shutdown both HTTP servers
	var wg sync.WaitGroup

	// Shutdown main HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Debugf("Attempting to gracefully shut down main HTTP server (max %v)...", httpShutdownTimeout)
		if err := a.httpServer.Shutdown(httpShutdownCtx); err != nil {
			logrus.Debugf("Main HTTP server graceful shutdown timed out as expected, forcing remaining connections to close.")
			if closeErr := a.httpServer.Close(); closeErr != nil {
				logrus.Errorf("Error forcing main HTTP server to close: %v", closeErr)
			}
		}
		logrus.Info("Main HTTP server has been shut down.")
	}()

	// Shutdown proxy-only HTTP server if it exists
	if a.proxyHTTPServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logrus.Debugf("Attempting to gracefully shut down proxy-only HTTP server (max %v)...", httpShutdownTimeout)
			if err := a.proxyHTTPServer.Shutdown(httpShutdownCtx); err != nil {
				logrus.Debugf("Proxy-only HTTP server graceful shutdown timed out as expected, forcing remaining connections to close.")
				if closeErr := a.proxyHTTPServer.Close(); closeErr != nil {
					logrus.Errorf("Error forcing proxy-only HTTP server to close: %v", closeErr)
				}
			}
			logrus.Info("Proxy-only HTTP server has been shut down.")
		}()
	}

	// Wait for both HTTP servers to shutdown
	wg.Wait()

	// 使用原始的总超时 context 继续关闭其他后台服务
	stoppableServices := []func(context.Context){
		a.groupManager.Stop,
		a.settingsManager.Stop,
	}

	if serverConfig.IsMaster {
		stoppableServices = append(stoppableServices,
			a.cronChecker.Stop,
			a.logCleanupService.Stop,
			a.requestLogService.Stop,
		)
	}

	wg.Add(len(stoppableServices))

	for _, stopFunc := range stoppableServices {
		go func(stop func(context.Context)) {
			defer wg.Done()
			stop(ctx)
		}(stopFunc)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("All background services stopped.")
	case <-ctx.Done():
		logrus.Warn("Shutdown timed out, some services may not have stopped gracefully.")
	}

	if a.storage != nil {
		a.storage.Close()
	}

	logrus.Info("Server exited gracefully")
}
