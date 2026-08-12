package app

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/authn"
	"grc/internal/controlcatalog"
	"grc/internal/db"
	"grc/internal/dbsync"
	"grc/internal/doctemplate"
	"grc/internal/nfrenrich"
	"grc/internal/nfrlink"
	"grc/internal/policydocs"
	"grc/internal/reporting"
	"grc/internal/reports"
	"grc/internal/riskregister"
	"grc/internal/securitynfr"
	"grc/internal/user"
)

type Options struct {
	ListenAddr string
	// SQLitePath is used when DatabaseURL is empty (the default, embedded
	// SQLite backend).
	SQLitePath string
	// DatabaseURL, when set, selects the external PostgreSQL backend and is a
	// standard connection string (postgres://user:pass@host:5432/db?sslmode=...).
	DatabaseURL       string
	AllowJSONSave     bool
	AdminToken        string
	LocalMode         bool
	TrustProxyHeaders bool
	// SyncTo / SyncFrom put the binary into one-shot database-sync mode instead
	// of serving: SyncTo copies the active backend up to the given target
	// spec, SyncFrom copies the given source spec down into the active backend.
	// A spec is a postgres:// URL or a SQLite file path. At most one may be set.
	SyncTo   string
	SyncFrom string
	// DocsDir is the directory holding the repository markdown files served by
	// /changelog and /knowledge/*. Empty means search (see docs.go); when set
	// it is used as-is so a wrong path reports an error rather than silently
	// falling back to another copy.
	DocsDir string
	// TemplatesDir is the directory holding the Typst/LaTeX document templates
	// rendered by /templates. Empty means search (see doctemplate.Dir); as with
	// DocsDir, a value set here is used as-is.
	TemplatesDir string
}

func DefaultOptions() Options {
	return Options{
		ListenAddr: listenAddr,
		SQLitePath: sqliteDBPath,
	}
}

func Run(options Options) error {
	if options.ListenAddr == "" {
		options.ListenAddr = listenAddr
	}
	if options.SQLitePath == "" {
		options.SQLitePath = sqliteDBPath
	}

	if options.SyncTo != "" && options.SyncFrom != "" {
		return fmt.Errorf("only one of -sync-to / -sync-from may be set")
	}

	var sqliteDB *db.Conn
	var err error
	if options.DatabaseURL != "" {
		sqliteDB, err = db.OpenPostgres(options.DatabaseURL)
	} else {
		sqliteDB, err = db.OpenSQLite(options.SQLitePath)
	}
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer sqliteDB.Close()

	if options.SyncTo != "" || options.SyncFrom != "" {
		return runDBSync(options, sqliteDB)
	}

	configureAIUsageStore(sqliteDB)
	configureStoredJSONStore(sqliteDB)

	localModeEnabled = options.LocalMode
	trustProxyHeaders = options.TrustProxyHeaders
	docsDirOverride = options.DocsDir
	doctemplate.DirOverride = options.TemplatesDir

	userHandler, authService, authHandler, controlHandler, securityNFRHandler, nfrLinkHandler, reportsHandler, riskRegisterHandler, err := buildHandlers(sqliteDB)
	if err != nil {
		return err
	}
	if authHandler != nil {
		authHandler.SetTrustProxyHeaders(options.TrustProxyHeaders)
	}

	adminMiddleware := adminTokenMiddleware(authService, options.AdminToken)
	if options.LocalMode {
		adminMiddleware = noAuthMiddleware
	}

	router := gin.Default()
	router.Use(themeMiddleware())
	if !options.LocalMode {
		router.Use(pageAccessMiddleware(authService))
	}
	registerRoutes(
		router,
		userHandler,
		authHandler,
		controlHandler,
		securityNFRHandler,
		nfrLinkHandler,
		reportsHandler,
		riskRegisterHandler,
		adminMiddleware,
		options.AllowJSONSave,
		options.LocalMode,
	)
	registerReportingRoutes(router)
	policyService := registerPolicyDocRoutes(router, sqliteDB, authService, adminMiddleware, options.LocalMode)
	registerDocTemplateRoutes(router, sqliteDB, policyService, adminMiddleware, options.LocalMode)
	registerNFREnrichmentRoutes(
		router, sqliteDB, securityNFRHandler.Service(), adminMiddleware, options.LocalMode)
	registerUtilitiesRoutes(router, sqliteDB, adminMiddleware, options.LocalMode)

	if err := router.Run(options.ListenAddr); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}
	return nil
}

// runDBSync executes a one-shot data sync and returns (the binary then exits
// rather than serving). The active backend is whichever -database-url /
// -sqlite-path selected; -sync-to copies it up to the target spec, -sync-from
// copies the source spec down into it. The "other" spec is engine-detected by
// db.Open (postgres:// URL vs SQLite path).
func runDBSync(options Options, active *db.Conn) error {
	var src, dst *db.Conn
	var direction string

	if options.SyncTo != "" {
		other, err := db.Open(options.SyncTo)
		if err != nil {
			return fmt.Errorf("failed to open sync target %q: %w", options.SyncTo, err)
		}
		defer other.Close()
		src, dst = active, other
		direction = "active -> " + options.SyncTo
	} else {
		other, err := db.Open(options.SyncFrom)
		if err != nil {
			return fmt.Errorf("failed to open sync source %q: %w", options.SyncFrom, err)
		}
		defer other.Close()
		src, dst = other, active
		direction = options.SyncFrom + " -> active"
	}

	log.Printf("database sync starting (%s)", direction)
	report, err := dbsync.Sync(src, dst)
	for _, t := range report.Tables {
		log.Printf("  %-28s %6d rows", t.Table, t.Rows)
	}
	if err != nil {
		return fmt.Errorf("database sync failed: %w", err)
	}
	log.Printf("database sync complete: %d rows across %d tables", report.Total(), len(report.Tables))
	return nil
}

func buildHandlers(sqliteDB *db.Conn) (*user.Handler, *authn.Service, *authn.Handler, *controlcatalog.Handler, *securitynfr.Handler, *nfrlink.Handler, *reports.Handler, *riskregister.Handler, error) {
	userRepo := user.NewSQLiteRepository(sqliteDB)
	userService := user.NewService(userRepo)
	userHandler := user.NewHandler(userService)
	authService := authn.NewService(sqliteDB)
	authHandler := authn.NewHandler(authService)

	controlRepo := controlcatalog.NewSQLiteRepository(sqliteDB)
	controlService := controlcatalog.NewService(controlRepo, controlDataPath)
	controlsChanged, err := syncControlCatalog(sqliteDB, controlService)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to sync flat control catalog: %w", err)
	}
	nfrLinkService := nfrlink.NewService(sqliteDB)
	controlHandler := controlcatalog.NewHandler(controlService, nfrLinkService)

	securityNFRRepo := securitynfr.NewSQLiteRepository(sqliteDB)
	securityNFRService := securitynfr.NewService(securityNFRRepo, securityNFRDataPath)
	nfrsChanged, err := syncSecurityNFRCatalog(sqliteDB, securityNFRService)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to sync security NFR catalog: %w", err)
	}
	securityNFRHandler := securitynfr.NewHandler(securityNFRService, nfrLinkService)

	if controlsChanged || nfrsChanged {
		if err := nfrLinkService.Rebuild(); err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to seed NFR-control linking table: %w", err)
		}
	}
	nfrLinkHandler := nfrlink.NewHandler(nfrLinkService)
	reportsService := reports.NewService(sqliteDB)
	reportsHandler := reports.NewHandler(reportsService)
	riskRegisterRepo := riskregister.NewSQLiteRepository(sqliteDB)
	riskRegisterService := riskregister.NewService(riskRegisterRepo)
	riskRegisterHandler := riskregister.NewHandler(riskRegisterService)

	return userHandler, authService, authHandler, controlHandler, securityNFRHandler, nfrLinkHandler, reportsHandler, riskRegisterHandler, nil
}

func syncControlCatalog(sqliteDB *db.Conn, service *controlcatalog.Service) (bool, error) {
	return syncSeedState(sqliteDB, "seed.control_catalog_hash", "rcsa_controls", service.SourceHash, service.Seed)
}

func syncSecurityNFRCatalog(sqliteDB *db.Conn, service *securitynfr.Service) (bool, error) {
	return syncSeedState(sqliteDB, "seed.security_nfr_hash", "security_nfrs", service.SourceHash, service.Seed)
}

func syncSeedState(sqliteDB *db.Conn, stateKey string, tableName string, sourceHash func() (string, error), seed func() (int, error)) (bool, error) {
	currentHash, err := sourceHash()
	if err != nil {
		return false, err
	}

	storedHash, err := loadAppState(sqliteDB, stateKey)
	if err != nil {
		return false, err
	}

	hasRows, err := tableHasRows(sqliteDB, tableName)
	if err != nil {
		return false, err
	}

	if hasRows && storedHash == currentHash {
		return false, nil
	}

	if _, err := seed(); err != nil {
		return false, err
	}
	if err := saveAppState(sqliteDB, stateKey, currentHash); err != nil {
		return false, err
	}
	return true, nil
}

func loadAppState(sqliteDB *db.Conn, key string) (string, error) {
	var value string
	err := sqliteDB.QueryRow(`SELECT value FROM app_state WHERE key = ?`, key).Scan(&value)
	if err == nil {
		return value, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return "", err
}

func saveAppState(sqliteDB *db.Conn, key string, value string) error {
	_, err := sqliteDB.Exec(`
		INSERT INTO app_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func tableHasRows(sqliteDB *db.Conn, tableName string) (bool, error) {
	var count int
	switch tableName {
	case "rcsa_controls":
		if err := sqliteDB.QueryRow(`SELECT COUNT(*) FROM rcsa_controls`).Scan(&count); err != nil {
			return false, err
		}
	case "security_nfrs":
		if err := sqliteDB.QueryRow(`SELECT COUNT(*) FROM security_nfrs`).Scan(&count); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported table name: %s", tableName)
	}
	return count > 0, nil
}

func registerRoutes(r gin.IRouter, userHandler *user.Handler, authHandler *authn.Handler, controlHandler *controlcatalog.Handler, securityNFRHandler *securitynfr.Handler, nfrLinkHandler *nfrlink.Handler, reportsHandler *reports.Handler, riskRegisterHandler *riskregister.Handler, adminMiddleware gin.HandlerFunc, allowJSONSave bool, localMode bool) {
	registerPublicPageRoutes(r, localMode)
	registerAIAuxRoutes(r)
	if !localMode {
		registerAuthRoutes(r, authHandler)
	}
	registerDomainReadRoutes(r, userHandler, controlHandler, securityNFRHandler, nfrLinkHandler, reportsHandler, riskRegisterHandler)
	registerAdminRoutes(r, userHandler, authHandler, controlHandler, securityNFRHandler, nfrLinkHandler, riskRegisterHandler, adminMiddleware, allowJSONSave, localMode)
}

// registerReportingRoutes wires the HTML/CSS-to-PDF reporting module. The
// headless browser starts lazily on the first report request and is pooled for
// the process lifetime. Chrome behavior can be tuned via REPORTING_CHROME_PATH
// and REPORTING_CHROME_NO_SANDBOX (see internal/reporting/README.md).
func registerReportingRoutes(r gin.IRouter) {
	var chromeOpts []reporting.ChromeOption
	if path := envOrDefault("REPORTING_CHROME_PATH", ""); path != "" {
		chromeOpts = append(chromeOpts, reporting.WithChromePath(path))
	}
	if envBoolOrDefault("REPORTING_CHROME_NO_SANDBOX", false) {
		chromeOpts = append(chromeOpts, reporting.WithNoSandbox(true))
	}
	renderer := reporting.NewChromeRenderer(chromeOpts...)
	service := reporting.NewService(renderer, nil)
	handler := reporting.NewHandler(service, nil)
	handler.RegisterRoutes(r)
}

// registerPolicyDocRoutes wires the policy-authoring module, following the same
// read-open / write-admin split as the CRM module. See
// POLICY_MODULE_FRAMEWORK.md — this is phase 1 (document model, editor,
// document control, approval) plus phase 5's rendering, which is wired up
// separately in registerDocTemplateRoutes. No AI or corpus yet.
//
// It returns the service so the template module can read a document without
// importing this one's handler.
func registerPolicyDocRoutes(r gin.IRouter, sqliteDB *db.Conn, authService *authn.Service, adminMiddleware gin.HandlerFunc, localMode bool) *policydocs.Service {
	policyService := policydocs.NewService(policydocs.NewSQLiteRepository(sqliteDB))

	// Separation of duties is only meaningful when there can be more than one
	// person: in single-user mode the author is necessarily the approver, and
	// enforcing it there would make every document permanently unapprovable.
	// Local mode has no identities at all.
	multiUser := !localMode && authService != nil && !authService.SingleUserMode()
	policyService.SetRequireSeparateApprover(multiUser)

	policyHandler := policydocs.NewHandler(policyService, sessionUsername)
	policyHandler.RegisterReadRoutes(r)

	admin := r.Group("/")
	if !localMode {
		admin.Use(adminMiddleware)
	}
	policyHandler.RegisterAdminRoutes(admin)
	return policyService
}

// registerDocTemplateRoutes wires the document-template module: the gallery,
// the brand editor and the render endpoint. POLICY_MODULE_FRAMEWORK.md phase 5.
//
// The policy service is adapted into a payload source here rather than being
// imported by internal/doctemplate, which keeps that package free of any
// knowledge of how a policy is stored — see its package comment. This function
// is the only place the two modules meet.
func registerDocTemplateRoutes(r gin.IRouter, sqliteDB *db.Conn, policyService *policydocs.Service, adminMiddleware gin.HandlerFunc, localMode bool) {
	templateService := doctemplate.NewService(doctemplate.NewSQLiteRepository(sqliteDB))
	templateService.SetPayloadSource(doctemplate.PolicyPayloadSource(func(id int64) (any, string, error) {
		export, err := policyService.ExportTemplateJSON(id)
		if err != nil {
			return nil, "", err
		}
		// The reference is the filename stem when there is one: a client
		// receiving POL-AC-001.pdf can file it without opening it. The title is
		// the fallback for a document that has not been given a reference yet.
		baseName := export.Reference
		if strings.TrimSpace(baseName) == "" {
			baseName = export.Title
		}
		return export, baseName, nil
	}))

	templateHandler := doctemplate.NewHandler(templateService, sessionUsername)
	templateHandler.RegisterReadRoutes(r)

	admin := r.Group("/")
	if !localMode {
		admin.Use(adminMiddleware)
	}
	templateHandler.RegisterAdminRoutes(admin)
}

// registerNFREnrichmentRoutes wires the security-document enrichment module.
//
// The read/write split matches the other RCSA catalog modules rather than the
// personal ones: reading the proposal queue is open to any signed-in user, but
// ingesting a document, running an analysis and accepting a proposal all sit
// behind the admin middleware. Accepting is the reason — it writes to the
// security NFR catalog, which is a client deliverable, and "AI drafted it" is
// not a weaker claim on that write than a hand edit, it is a stronger reason to
// gate it. Running an analysis is gated too, because it spends money.
//
// The analyzer is handed logAIUsage so this module's token spend lands in the
// same ai_usage_log the AI Chat gateway writes to — one place for spend, per
// POLICY_MODULE_FRAMEWORK.md §2.5.
func registerNFREnrichmentRoutes(
	r gin.IRouter,
	sqliteDB *db.Conn,
	nfrService *securitynfr.Service,
	adminMiddleware gin.HandlerFunc,
	localMode bool,
) {
	service := nfrenrich.NewService(
		nfrenrich.NewSQLiteRepository(sqliteDB),
		nfrService,
		nfrenrich.NewClaudeAnalyzer(logAIUsage),
	)
	handler := nfrenrich.NewHandler(service, sessionUsername)
	handler.RegisterRoutes(r)

	admin := r.Group("/")
	if !localMode {
		admin.Use(adminMiddleware)
	}
	handler.RegisterAdminRoutes(admin)
}

func registerPublicPageRoutes(r gin.IRouter, localMode bool) {
	r.GET("/", homePage)
	if !localMode {
		r.GET("/login", loginPage)
	}
	r.GET("/version", versionHandler)
	r.GET("/JSON_view", jsonViewPage)
	r.GET("/json-view", jsonViewPage)
	r.GET("/jira/json", jiraJSONPage)
	r.GET("/jira/reports", jiraReportsPage)
	r.GET("/knowledge/:name", markdownKnowledgeDocPage)
	r.GET("/changelog", changeLogPage)
	r.GET("/asset-types", assetTypesPage)
	r.GET("/exceptions", exceptionsPage)
	r.GET("/exceptions/detail", exceptionsDetailPage)
	r.GET("/ai-chat", aiChatPage)
	r.GET("/wiz-rules", wizRulesPage)
	r.GET("/docs", openAPIPage)
	r.GET("/openapi.json", openAPIJSON)
}

func registerAIAuxRoutes(r gin.IRouter) {
	r.POST("/ai-chat/ask", aiChatAsk)
	r.GET("/ai-chat/usage", aiChatUsageHandler)
	r.GET("/ai-chat/wintermute/status", aiChatWintermuteStatus)
}

func registerAuthRoutes(r gin.IRouter, authHandler *authn.Handler) {
	r.POST("/auth/bootstrap-admin", authHandler.BootstrapAdmin)
	r.POST("/auth/login", authHandler.Login)
	r.POST("/auth/logout", authHandler.Logout)
	r.GET("/auth/me", authHandler.Me)
}

func registerDomainReadRoutes(r gin.IRouter, userHandler *user.Handler, controlHandler *controlcatalog.Handler, securityNFRHandler *securitynfr.Handler, nfrLinkHandler *nfrlink.Handler, reportsHandler *reports.Handler, riskRegisterHandler *riskregister.Handler) {
	r.GET("/controls", controlHandler.Page)
	r.GET("/controls/detail/:controlID", controlHandler.DetailPage)
	r.GET("/controls/manage", controlHandler.ManagePage)
	r.GET("/controls/family-visibility", controlHandler.FamilyVisibilityPage)
	r.GET("/controls/hierarchy", controlHandler.HierarchyPage)
	r.GET("/controls/hierarchy/export", controlHandler.ExportHierarchyRTF)
	r.GET("/controls/family-json/data", controlHandler.FamilyJSONData)
	r.GET("/controls/family-visibility/data", controlHandler.ListFamilyVisibility)
	r.GET("/controls/data", controlHandler.ListControls)
	r.GET("/controls/data/:controlID", controlHandler.GetControl)
	r.GET("/controls/export", controlHandler.ExportControls)

	r.GET("/security-nfrs", securityNFRHandler.Page)
	r.GET("/security-nfrs/manage", securityNFRHandler.ManagePage)
	r.GET("/security-nfrs/detail/:key", securityNFRHandler.DetailPage)
	r.GET("/security-nfrs/data", securityNFRHandler.ListNFRs)
	r.GET("/security-nfrs/json/data", securityNFRHandler.NFRJSONData)
	r.GET("/security-nfrs/domains", securityNFRHandler.ListDomains)
	r.GET("/security-nfrs/links", nfrLinkHandler.Page)
	r.GET("/security-nfrs/links/data", nfrLinkHandler.ListLinkRows)
	r.GET("/reports", reportsHandler.Page)
	r.GET("/reports/data/unlinked-controls", reportsHandler.UnlinkedControlsData)
	r.GET("/jira/reports/presets", jiraReportPresetsData)
	r.POST("/jira/json/test-auth", jiraJSONTestAuth)
	r.POST("/jira/json/data", jiraJSONData)
	r.POST("/jira/reports/data", jiraReportsData)
	r.GET("/stored-json", listStoredJSONDocuments)
	r.GET("/stored-json/:id", getStoredJSONDocument)
	r.POST("/stored-json", saveStoredJSONDocument)
	r.GET("/risk-register", riskRegisterHandler.Page)
	r.GET("/risk-register/manage", riskRegisterHandler.ManagePage)
	r.GET("/risk-register/data", riskRegisterHandler.List)

	r.GET("/health", userHandler.Health)
	r.GET("/users", userHandler.ListUsers)
	r.GET("/users/:id", userHandler.GetUser)
}

func registerAdminRoutes(r gin.IRouter, userHandler *user.Handler, authHandler *authn.Handler, controlHandler *controlcatalog.Handler, securityNFRHandler *securitynfr.Handler, nfrLinkHandler *nfrlink.Handler, riskRegisterHandler *riskregister.Handler, adminMiddleware gin.HandlerFunc, allowJSONSave bool, localMode bool) {
	admin := r.Group("/")
	admin.Use(adminMiddleware)
	if !localMode {
		admin.GET("/admin/user-management", userManagementPage)
		admin.GET("/auth/users", authHandler.ListUsers)
		admin.POST("/auth/users", authHandler.CreateUser)
		admin.PUT("/auth/users/:username", authHandler.UpdateUser)
		admin.DELETE("/auth/users/:username", authHandler.DeleteUser)
	}
	admin.POST("/users", userHandler.CreateUser)
	admin.PUT("/users/:id", userHandler.UpdateUser)
	admin.DELETE("/users/:id", userHandler.DeleteUser)
	admin.PUT("/controls/family-visibility/:family", controlHandler.UpdateFamilyVisibility)
	admin.PUT("/controls/:controlID", controlHandler.UpdateControl)
	admin.DELETE("/controls/:controlID", controlHandler.DeleteControl)
	admin.PUT("/security-nfrs/:key", securityNFRHandler.UpdateNFR)
	admin.DELETE("/security-nfrs/:key", securityNFRHandler.DeleteNFR)
	admin.GET("/security-nfrs/links/overrides", nfrLinkHandler.ListOverrides)
	admin.PUT("/security-nfrs/links/overrides", nfrLinkHandler.SetOverride)
	admin.DELETE("/security-nfrs/links/overrides/:nfrKey/:mappingControlID", nfrLinkHandler.DeleteOverride)
	admin.POST("/security-nfrs/links/rebuild", nfrLinkHandler.Rebuild)
	admin.POST("/risk-register", riskRegisterHandler.Create)
	admin.PUT("/risk-register/:id", riskRegisterHandler.Update)
	admin.DELETE("/risk-register/:id", riskRegisterHandler.Delete)
	if allowJSONSave {
		admin.POST("/controls/save", controlHandler.SaveControlsToJSON)
		admin.POST("/security-nfrs/save", securityNFRHandler.SaveNFRsToJSON)
		admin.POST("/jira/json/export", jiraJSONExport)
	}
}
