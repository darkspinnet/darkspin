export namespace main {
  export class RemoteControlAccessResult {
    status: number;
    ip_limit: string;
    is_disconnect_pending: boolean;
    static createFrom(source: any = {}) { return new RemoteControlAccessResult(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.status = source['status'];
      this.ip_limit = source['ip_limit'];
      this.is_disconnect_pending = source['is_disconnect_pending'];
    }
  }

	
	export class ReportResult {
	    name: string;
	    names: string[];
	    directory: string;
	    fileCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ReportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.names = source["names"];
	        this.directory = source["directory"];
	        this.fileCount = source["fileCount"];
	    }
	}
	export class AutomaticClientReport {
	    launchId: string;
	    title: string;
	    message: string;
	    isCrash: boolean;
	    isPreparing: boolean;
	    result: ReportResult;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new AutomaticClientReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.launchId = source["launchId"];
	        this.title = source["title"];
	        this.message = source["message"];
	        this.isCrash = source["isCrash"];
	        this.isPreparing = source["isPreparing"];
	        this.result = this.convertValues(source["result"], ReportResult);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ClientLocale {
	    code: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new ClientLocale(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.label = source["label"];
	    }
	}
	export class InstallationStatus {
	    isLocalReady: boolean;
	    isSteamInstalled: boolean;
	    isGameInstalled: boolean;
	    canRelocate: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new InstallationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isLocalReady = source["isLocalReady"];
	        this.isSteamInstalled = source["isSteamInstalled"];
	        this.isGameInstalled = source["isGameInstalled"];
	        this.canRelocate = source["canRelocate"];
	        this.message = source["message"];
	    }
	}
	export class InterruptedMission {
	    gameId: number;
	    level: string;
	    label: string;
	    difficulty: number;
	    isAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InterruptedMission(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gameId = source["gameId"];
	        this.level = source["level"];
	        this.label = source["label"];
	        this.difficulty = source["difficulty"];
	        this.isAvailable = source["isAvailable"];
	    }
	}
	export class LauncherAutomationConfiguration {
	    isAutoPatchEnabled?: boolean;
	    isAutoPlayEnabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LauncherAutomationConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isAutoPatchEnabled = source["isAutoPatchEnabled"];
	        this.isAutoPlayEnabled = source["isAutoPlayEnabled"];
	    }
	}
	export class LauncherIntegrationStatus {
	    isStartMenuInstalled: boolean;
	    isDesktopInstalled: boolean;
	    isStartMenuOwned: boolean;
	    isDesktopOwned: boolean;
	    isSteamLaunchInstalled: boolean;
	    isManagementSupported: boolean;
	    message: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new LauncherIntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isStartMenuInstalled = source["isStartMenuInstalled"];
	        this.isDesktopInstalled = source["isDesktopInstalled"];
	        this.isStartMenuOwned = source["isStartMenuOwned"];
	        this.isDesktopOwned = source["isDesktopOwned"];
	        this.isSteamLaunchInstalled = source["isSteamLaunchInstalled"];
	        this.isManagementSupported = source["isManagementSupported"];
	        this.message = source["message"];
	        this.error = source["error"];
	    }
	}
	export class LauncherLanguageConfiguration {
	    locale: string;
	    locales: ClientLocale[];
	
	    static createFrom(source: any = {}) {
	        return new LauncherLanguageConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.locale = source["locale"];
	        this.locales = this.convertValues(source["locales"], ClientLocale);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LauncherStatus {
	    automaticClientReport?: AutomaticClientReport;
	    state: string;
	    message: string;
	    identity: string;
	    auth: string;
	    server: string;
	    patch: string;
	    game: string;
	    avatar: string;
	    profile: string;
	    content: string;
	    identityError: string;
	    authError: string;
	    serverError: string;
	    patchError: string;
	    gameError: string;
	    avatarError: string;
	    profileError: string;
	    contentError: string;
	    lastRun: string;
	    clientLoginFailure: string;
	    clientLoginFailureId: string;
	    launcherNotice: string;
	    manifestUrl: string;
	    gameDirectory: string;
	    version: string;
	    buildChannel: string;
	    progress: number;
	    patchProgress: number;
	    avatarProgress: number;
	    contentProgress: number;
	    requiredFile: number;
	    deleteFile: number;
	    downloadByte: number;
	    isAuthenticated: boolean;
	    isAuthOnline: boolean;
	    isServerOnline: boolean;
	    isPatchComplete: boolean;
	    isPatchActive: boolean;
	    isGameReady: boolean;
	    isAvatarReady: boolean;
	    isProfileStoreReady: boolean;
	    isContentReady: boolean;
	    isPlayReady: boolean;
	    isAutoPlayRequested: boolean;
	    isPatchEnabled: boolean;
	    isCinematicSkipped: boolean;
	    isIntroSkipped: boolean;
	    isLastRunFailure: boolean;
	    isStartupBlocked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LauncherStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.automaticClientReport = this.convertValues(source["automaticClientReport"], AutomaticClientReport);
	        this.state = source["state"];
	        this.message = source["message"];
	        this.identity = source["identity"];
	        this.auth = source["auth"];
	        this.server = source["server"];
	        this.patch = source["patch"];
	        this.game = source["game"];
	        this.avatar = source["avatar"];
	        this.profile = source["profile"];
	        this.content = source["content"];
	        this.identityError = source["identityError"];
	        this.authError = source["authError"];
	        this.serverError = source["serverError"];
	        this.patchError = source["patchError"];
	        this.gameError = source["gameError"];
	        this.avatarError = source["avatarError"];
	        this.profileError = source["profileError"];
	        this.contentError = source["contentError"];
	        this.lastRun = source["lastRun"];
	        this.clientLoginFailure = source["clientLoginFailure"];
	        this.clientLoginFailureId = source["clientLoginFailureId"];
	        this.launcherNotice = source["launcherNotice"];
	        this.manifestUrl = source["manifestUrl"];
	        this.gameDirectory = source["gameDirectory"];
	        this.version = source["version"];
	        this.buildChannel = source["buildChannel"];
	        this.progress = source["progress"];
	        this.patchProgress = source["patchProgress"];
	        this.avatarProgress = source["avatarProgress"];
	        this.contentProgress = source["contentProgress"];
	        this.requiredFile = source["requiredFile"];
	        this.deleteFile = source["deleteFile"];
	        this.downloadByte = source["downloadByte"];
	        this.isAuthenticated = source["isAuthenticated"];
	        this.isAuthOnline = source["isAuthOnline"];
	        this.isServerOnline = source["isServerOnline"];
	        this.isPatchComplete = source["isPatchComplete"];
	        this.isPatchActive = source["isPatchActive"];
	        this.isGameReady = source["isGameReady"];
	        this.isAvatarReady = source["isAvatarReady"];
	        this.isProfileStoreReady = source["isProfileStoreReady"];
	        this.isContentReady = source["isContentReady"];
	        this.isPlayReady = source["isPlayReady"];
	        this.isAutoPlayRequested = source["isAutoPlayRequested"];
	        this.isPatchEnabled = source["isPatchEnabled"];
	        this.isCinematicSkipped = source["isCinematicSkipped"];
	        this.isIntroSkipped = source["isIntroSkipped"];
	        this.isLastRunFailure = source["isLastRunFailure"];
	        this.isStartupBlocked = source["isStartupBlocked"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LuaConsoleLine {
	    sequence: number;
	    time: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new LuaConsoleLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.time = source["time"];
	        this.text = source["text"];
	    }
	}
	export class LuaScriptChoice {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new LuaScriptChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class LuaConsoleSnapshot {
	    isAvailable: boolean;
	    isSelected: boolean;
	    isActive: boolean;
	    phase: string;
	    script: string;
	    runId: string;
	    elapsedMs: number;
	    failure: string;
	    inventoryError: string;
	    scripts: LuaScriptChoice[];
	    lines: LuaConsoleLine[];
	
	    static createFrom(source: any = {}) {
	        return new LuaConsoleSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isAvailable = source["isAvailable"];
	        this.isSelected = source["isSelected"];
	        this.isActive = source["isActive"];
	        this.phase = source["phase"];
	        this.script = source["script"];
	        this.runId = source["runId"];
	        this.elapsedMs = source["elapsedMs"];
	        this.failure = source["failure"];
	        this.inventoryError = source["inventoryError"];
	        this.scripts = this.convertValues(source["scripts"], LuaScriptChoice);
	        this.lines = this.convertValues(source["lines"], LuaConsoleLine);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ModBinding {
	    heroNoun: number;
	    creationId: string;
	
	    static createFrom(source: any = {}) {
	        return new ModBinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.heroNoun = source["heroNoun"];
	        this.creationId = source["creationId"];
	    }
	}
	export class ModProfile {
	    loginName: string;
	    displayName: string;
	    createDt: string;
	    lastConnectionDt: string;
	    crogenitorLevel: number;
	    cumulativeXp: number;
	    highestCampaignUnlocked: number;
	    isTutorialCompleted: boolean;
	    isTutorialCompletionPending: boolean;
	    avatarId: number;
	    avatarUrl: string;
	    bindings: ModBinding[];
	
	    static createFrom(source: any = {}) {
	        return new ModProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loginName = source["loginName"];
	        this.displayName = source["displayName"];
	        this.createDt = source["createDt"];
	        this.lastConnectionDt = source["lastConnectionDt"];
	        this.crogenitorLevel = source["crogenitorLevel"];
	        this.cumulativeXp = source["cumulativeXp"];
	        this.highestCampaignUnlocked = source["highestCampaignUnlocked"];
	        this.isTutorialCompleted = source["isTutorialCompleted"];
	        this.isTutorialCompletionPending = source["isTutorialCompletionPending"];
	        this.avatarId = source["avatarId"];
	        this.avatarUrl = source["avatarUrl"];
	        this.bindings = this.convertValues(source["bindings"], ModBinding);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModHero {
	    noun: number;
	    name: string;
	    portraitUrl: string;
	    category: string;
	    nounName: string;
	    nominalScale: number;
	    targetExtent: number;
	
	    static createFrom(source: any = {}) {
	        return new ModHero(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.noun = source["noun"];
	        this.name = source["name"];
	        this.portraitUrl = source["portraitUrl"];
	        this.category = source["category"];
	        this.nounName = source["nounName"];
	        this.nominalScale = source["nominalScale"];
	        this.targetExtent = source["targetExtent"];
	    }
	}
	export class ModCreature {
	    id: string;
	    name: string;
	    isPersonal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModCreature(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.isPersonal = source["isPersonal"];
	    }
	}
	export class ModConfiguration {
	    sporePath: string;
	    creatures: ModCreature[];
	    heroes: ModHero[];
	    profiles: ModProfile[];
	    sourceError: string;
	
	    static createFrom(source: any = {}) {
	        return new ModConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sporePath = source["sporePath"];
	        this.creatures = this.convertValues(source["creatures"], ModCreature);
	        this.heroes = this.convertValues(source["heroes"], ModHero);
	        this.profiles = this.convertValues(source["profiles"], ModProfile);
	        this.sourceError = source["sourceError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ModDirectory {
	    path: string;
	    parent: string;
	    directories: string[];
	    executables: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModDirectory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.parent = source["parent"];
	        this.directories = source["directories"];
	        this.executables = source["executables"];
	    }
	}
	
	
	export class ModProfileRequest {
	    loginName: string;
	    avatarId: number;
	    isTutorialSkipped: boolean;
	    isNew: boolean;
	    isResyncRequested: boolean;
	    bindings: ModBinding[];
	
	    static createFrom(source: any = {}) {
	        return new ModProfileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loginName = source["loginName"];
	        this.avatarId = source["avatarId"];
	        this.isTutorialSkipped = source["isTutorialSkipped"];
	        this.isNew = source["isNew"];
	        this.isResyncRequested = source["isResyncRequested"];
	        this.bindings = this.convertValues(source["bindings"], ModBinding);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Profile {
	    loginName: string;
	    displayName: string;
	    createDt: string;
	    lastConnectionDt: string;
	    avatarId: number;
	    avatarUrl: string;
	    crogenitorLevel: number;
	    cumulativeXp: number;
	    highestCampaignUnlocked: number;
	    isTutorialCompleted: boolean;
	    isTutorialCompletionPending: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loginName = source["loginName"];
	        this.displayName = source["displayName"];
	        this.createDt = source["createDt"];
	        this.lastConnectionDt = source["lastConnectionDt"];
	        this.avatarId = source["avatarId"];
	        this.avatarUrl = source["avatarUrl"];
	        this.crogenitorLevel = source["crogenitorLevel"];
	        this.cumulativeXp = source["cumulativeXp"];
	        this.highestCampaignUnlocked = source["highestCampaignUnlocked"];
	        this.isTutorialCompleted = source["isTutorialCompleted"];
	        this.isTutorialCompletionPending = source["isTutorialCompletionPending"];
	    }
	}
	export class ProfileAvatar {
	    id: number;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new ProfileAvatar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	    }
	}
	export class RelocationRequest {
	    isStartMenuShortcut: boolean;
	    isDesktopShortcut: boolean;
	    isSteamLaunch: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RelocationRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isStartMenuShortcut = source["isStartMenuShortcut"];
	        this.isDesktopShortcut = source["isDesktopShortcut"];
	        this.isSteamLaunch = source["isSteamLaunch"];
	    }
	}
	export class RemoteControlAccountSample {
	    at: string;
	    account_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlAccountSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.account_count = source["account_count"];
	    }
	}
	export class RemoteControlActionResult {
	    display_name: string;
	    is_banned: boolean;
	    is_timed_out: boolean;
	    timeout_until_unix: number;
	    is_disconnect_pending: boolean;
	    is_cache_updated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.display_name = source["display_name"];
	        this.is_banned = source["is_banned"];
	        this.is_timed_out = source["is_timed_out"];
	        this.timeout_until_unix = source["timeout_until_unix"];
	        this.is_disconnect_pending = source["is_disconnect_pending"];
	        this.is_cache_updated = source["is_cache_updated"];
	    }
	}
	export class RemoteControlActivitySample {
	    at: string;
	    unique_player_count: number;
	    peak_online_count?: number;
	    online_count?: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlActivitySample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.unique_player_count = source["unique_player_count"];
	        this.peak_online_count = source["peak_online_count"];
	        this.online_count = source["online_count"];
	    }
	}
	export class RemoteControlProgressionSample {
	    at: string;
	    observed_at: string;
	    average_campaign_progression?: number;
	    highest_completed_campaign?: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlProgressionSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.observed_at = source["observed_at"];
	        this.average_campaign_progression = source["average_campaign_progression"];
	        this.highest_completed_campaign = source["highest_completed_campaign"];
	    }
	}
	export class RemoteControlDNASample {
	    at: string;
	    earned: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlDNASample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.earned = source["earned"];
	    }
	}
	export class RemoteControlLevelSample {
	    at: string;
	    observed_at: string;
	    average_crogenitor_level?: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlLevelSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.observed_at = source["observed_at"];
	        this.average_crogenitor_level = source["average_crogenitor_level"];
	    }
	}
	export class RemoteControlCampaignSample {
	    at: string;
	    completion_count: number;
	    replay_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlCampaignSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.completion_count = source["completion_count"];
	        this.replay_count = source["replay_count"];
	    }
	}
	export class RemoteControlDistributionSample {
	    at: string;
	    pve_count: number;
	    pvp_count: number;
	    lobby_count: number;
	    matchmaking_count: number;
	    other_count: number;
	    observation_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlDistributionSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.pve_count = source["pve_count"];
	        this.pvp_count = source["pvp_count"];
	        this.lobby_count = source["lobby_count"];
	        this.matchmaking_count = source["matchmaking_count"];
	        this.other_count = source["other_count"];
	        this.observation_count = source["observation_count"];
	    }
	}
	export class RemoteControlQueueSample {
	    at: string;
	    kind: string;
	    queued_count: number;
	    average_wait_seconds?: number;
	    maximum_wait_seconds?: number;
	    observation_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlQueueSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.kind = source["kind"];
	        this.queued_count = source["queued_count"];
	        this.average_wait_seconds = source["average_wait_seconds"];
	        this.maximum_wait_seconds = source["maximum_wait_seconds"];
	        this.observation_count = source["observation_count"];
	    }
	}
	export class RemoteControlCampaignLevel {
	    campaign_number: number;
	    level_number: number;
	    completion_count: number;
	    repeat_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlCampaignLevel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.campaign_number = source["campaign_number"];
	        this.level_number = source["level_number"];
	        this.completion_count = source["completion_count"];
	        this.repeat_count = source["repeat_count"];
	    }
	}
	export class RemoteControlMissionSample {
	    at: string;
	    started_count: number;
	    completed_count: number;
	    unresolved_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlMissionSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.started_count = source["started_count"];
	        this.completed_count = source["completed_count"];
	        this.unresolved_count = source["unresolved_count"];
	    }
	}
	export class RemoteControlActivity {
	    mission_started_at: string;
	    mission_samples: RemoteControlMissionSample[];
	    campaign_level_started_at: string;
	    campaign_levels: RemoteControlCampaignLevel[];
	    queue_started_at: string;
	    queue_samples: RemoteControlQueueSample[];
	    distribution_started_at: string;
	    distribution_samples: RemoteControlDistributionSample[];
	    campaign_started_at: string;
	    campaign_samples: RemoteControlCampaignSample[];
	    signup_started_at: string;
	    signup_samples: RemoteControlAccountSample[];
	    first_admission_started_at: string;
	    first_admission_samples: RemoteControlAccountSample[];
	    population_observed_at: string;
	    population_samples: RemoteControlAccountSample[];
	    creation_samples: RemoteControlAccountSample[];
	    range: string;
	    observed_at: string;
	    daily_observed_at: string;
	    next_refresh_at: string;
	    range_start: string;
	    collection_started_at: string;
	    last_sample_at: string;
	    is_partial: boolean;
	    unique_player_count: number;
	    peak_online_count: number;
	    average_crogenitor_level?: number;
	    level_observed_at: string;
	    level_samples: RemoteControlLevelSample[];
	    dna_earned: number;
	    dna_collection_started_at: string;
	    is_dna_partial: boolean;
	    dna_samples: RemoteControlDNASample[];
	    average_campaign_progression?: number;
	    highest_completed_campaign?: number;
	    progression_observed_at: string;
	    progression_samples: RemoteControlProgressionSample[];
	    bucket_seconds: number;
	    samples: RemoteControlActivitySample[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlActivity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mission_started_at = source["mission_started_at"];
	        this.mission_samples = this.convertValues(source["mission_samples"], RemoteControlMissionSample);
	        this.campaign_level_started_at = source["campaign_level_started_at"];
	        this.campaign_levels = this.convertValues(source["campaign_levels"], RemoteControlCampaignLevel);
	        this.queue_started_at = source["queue_started_at"];
	        this.queue_samples = this.convertValues(source["queue_samples"], RemoteControlQueueSample);
	        this.distribution_started_at = source["distribution_started_at"];
	        this.distribution_samples = this.convertValues(source["distribution_samples"], RemoteControlDistributionSample);
	        this.campaign_started_at = source["campaign_started_at"];
	        this.campaign_samples = this.convertValues(source["campaign_samples"], RemoteControlCampaignSample);
	        this.signup_started_at = source["signup_started_at"];
	        this.signup_samples = this.convertValues(source["signup_samples"], RemoteControlAccountSample);
	        this.first_admission_started_at = source["first_admission_started_at"];
	        this.first_admission_samples = this.convertValues(source["first_admission_samples"], RemoteControlAccountSample);
	        this.population_observed_at = source["population_observed_at"];
	        this.population_samples = this.convertValues(source["population_samples"], RemoteControlAccountSample);
	        this.creation_samples = this.convertValues(source["creation_samples"], RemoteControlAccountSample);
	        this.range = source["range"];
	        this.observed_at = source["observed_at"];
	        this.daily_observed_at = source["daily_observed_at"];
	        this.next_refresh_at = source["next_refresh_at"];
	        this.range_start = source["range_start"];
	        this.collection_started_at = source["collection_started_at"];
	        this.last_sample_at = source["last_sample_at"];
	        this.is_partial = source["is_partial"];
	        this.unique_player_count = source["unique_player_count"];
	        this.peak_online_count = source["peak_online_count"];
	        this.average_crogenitor_level = source["average_crogenitor_level"];
	        this.level_observed_at = source["level_observed_at"];
	        this.level_samples = this.convertValues(source["level_samples"], RemoteControlLevelSample);
	        this.dna_earned = source["dna_earned"];
	        this.dna_collection_started_at = source["dna_collection_started_at"];
	        this.is_dna_partial = source["is_dna_partial"];
	        this.dna_samples = this.convertValues(source["dna_samples"], RemoteControlDNASample);
	        this.average_campaign_progression = source["average_campaign_progression"];
	        this.highest_completed_campaign = source["highest_completed_campaign"];
	        this.progression_observed_at = source["progression_observed_at"];
	        this.progression_samples = this.convertValues(source["progression_samples"], RemoteControlProgressionSample);
	        this.bucket_seconds = source["bucket_seconds"];
	        this.samples = this.convertValues(source["samples"], RemoteControlActivitySample);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RemoteControlAnnouncement {
	    id: string;
	    message: string;
	    created_at: string;
	    expires_at: string;
	    delivered: number;
	    is_delivery_partial: boolean;
	    is_audit_recorded: boolean;
	    outcome: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlAnnouncement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.message = source["message"];
	        this.created_at = source["created_at"];
	        this.expires_at = source["expires_at"];
	        this.delivered = source["delivered"];
	        this.is_delivery_partial = source["is_delivery_partial"];
	        this.is_audit_recorded = source["is_audit_recorded"];
	        this.outcome = source["outcome"];
	    }
	}
	export class RemoteControlAnnouncementList {
	    announcements: RemoteControlAnnouncement[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlAnnouncementList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.announcements = this.convertValues(source["announcements"], RemoteControlAnnouncement);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class RemoteControlDiagnostic {
	    name: string;
	    size: number;
	    modified_at: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlDiagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.modified_at = source["modified_at"];
	    }
	}
	export class RemoteControlDiagnosticDownload {
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlDiagnosticDownload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	    }
	}
	export class RemoteControlDiagnosticList {
	    files: RemoteControlDiagnostic[];
	    next: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlDiagnosticList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], RemoteControlDiagnostic);
	        this.next = source["next"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RemoteControlEvent {
	    id: number;
	    actor_type: string;
	    actor_user_id: number;
	    target_user_id: number;
	    action: string;
	    occurred_at_unix: number;
	    reason: string;
	    operation_id: string;
	    outcome: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.actor_type = source["actor_type"];
	        this.actor_user_id = source["actor_user_id"];
	        this.target_user_id = source["target_user_id"];
	        this.action = source["action"];
	        this.occurred_at_unix = source["occurred_at_unix"];
	        this.reason = source["reason"];
	        this.operation_id = source["operation_id"];
	        this.outcome = source["outcome"];
	    }
	}
	export class remoteControlBackupSuccess {
	    id: string;
	    started_at: string;
	    completed_at: string;
	    build_id: string;
	    age_seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlBackupSuccess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.started_at = source["started_at"];
	        this.completed_at = source["completed_at"];
	        this.build_id = source["build_id"];
	        this.age_seconds = source["age_seconds"];
	    }
	}
	export class remoteControlRestoreHealth {
	    status: string;
	    backup_id: string;
	    started_at: string;
	    updated_at: string;
	    restored_at: string;
	    healthy_at: string;
	    failure_code: string;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlRestoreHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.backup_id = source["backup_id"];
	        this.started_at = source["started_at"];
	        this.updated_at = source["updated_at"];
	        this.restored_at = source["restored_at"];
	        this.healthy_at = source["healthy_at"];
	        this.failure_code = source["failure_code"];
	    }
	}
	export class remoteControlBackupHealth {
	    recovery: remoteControlRestoreHealth;
	    status: string;
	    observed_at: string;
	    last_attempt_at: string;
	    last_failure_at: string;
	    failure_code: string;
	    last_success?: remoteControlBackupSuccess;
	    storage: remoteControlStorageHealth;
	    archive_status: string;
	    archive_observed_at: string;
	    integrity_status: string;
	    restore_status: string;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlBackupHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recovery = this.convertValues(source["recovery"], remoteControlRestoreHealth);
	        this.status = source["status"];
	        this.observed_at = source["observed_at"];
	        this.last_attempt_at = source["last_attempt_at"];
	        this.last_failure_at = source["last_failure_at"];
	        this.failure_code = source["failure_code"];
	        this.last_success = this.convertValues(source["last_success"], remoteControlBackupSuccess);
	        this.storage = this.convertValues(source["storage"], remoteControlStorageHealth);
	        this.archive_status = source["archive_status"];
	        this.archive_observed_at = source["archive_observed_at"];
	        this.integrity_status = source["integrity_status"];
	        this.restore_status = source["restore_status"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class remoteControlStorageHealth {
	    status: string;
	    observed_at: string;
	    total_bytes?: number;
	    available_bytes?: number;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlStorageHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.observed_at = source["observed_at"];
	        this.total_bytes = source["total_bytes"];
	        this.available_bytes = source["available_bytes"];
	    }
	}
	export class remoteControlProfileSaveHealth {
	    last_success_at: string;
	    last_failure_at: string;
	    pending_accounts: number;
	    failed_accounts: number;
	    is_pending_failure: boolean;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlProfileSaveHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.last_success_at = source["last_success_at"];
	        this.last_failure_at = source["last_failure_at"];
	        this.pending_accounts = source["pending_accounts"];
	        this.failed_accounts = source["failed_accounts"];
	        this.is_pending_failure = source["is_pending_failure"];
	    }
	}
	export class remoteControlObservation {
	    status: string;
	    observed_at: string;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlObservation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.observed_at = source["observed_at"];
	    }
	}
	export class RemoteControlHealth {
	    observed_at: string;
	    status: string;
	    database: remoteControlObservation;
	    profile_saves: remoteControlProfileSaveHealth;
	    storage: remoteControlStorageHealth;
	    backup: remoteControlBackupHealth;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.observed_at = source["observed_at"];
	        this.status = source["status"];
	        this.database = this.convertValues(source["database"], remoteControlObservation);
	        this.profile_saves = this.convertValues(source["profile_saves"], remoteControlProfileSaveHealth);
	        this.storage = this.convertValues(source["storage"], remoteControlStorageHealth);
	        this.backup = this.convertValues(source["backup"], remoteControlBackupHealth);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteControlHistory {
	    events: RemoteControlEvent[];
	    next_event_id: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlHistory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.events = this.convertValues(source["events"], RemoteControlEvent);
	        this.next_event_id = source["next_event_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteControlHistoryFilter {
	    before_event_id: number;
	    target_user_id: number;
	    actor_user_id: number;
	    actor_type: string;
	    after_unix: number;
	    before_unix: number;
	    action: string;
	    outcome: string;
	    operation_id: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlHistoryFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.before_event_id = source["before_event_id"];
	        this.target_user_id = source["target_user_id"];
	        this.actor_user_id = source["actor_user_id"];
	        this.actor_type = source["actor_type"];
	        this.after_unix = source["after_unix"];
	        this.before_unix = source["before_unix"];
	        this.action = source["action"];
	        this.outcome = source["outcome"];
	        this.operation_id = source["operation_id"];
	    }
	}
	
	export class RemoteControlLoginEntry {
	    ip_address: string;
	    first_seen_at: string;
	    last_seen_at: string;
	    login_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlLoginEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip_address = source["ip_address"];
	        this.first_seen_at = source["first_seen_at"];
	        this.last_seen_at = source["last_seen_at"];
	        this.login_count = source["login_count"];
	    }
	}
	export class RemoteControlLoginHistory {
	    user_id: number;
	    range: string;
	    range_start: string;
	    until: string;
	    entries: RemoteControlLoginEntry[];
	    next_ip: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlLoginHistory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user_id = source["user_id"];
	        this.range = source["range"];
	        this.range_start = source["range_start"];
	        this.until = source["until"];
	        this.entries = this.convertValues(source["entries"], RemoteControlLoginEntry);
	        this.next_ip = source["next_ip"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteControlMaintenanceResult {
	    operation_id: string;
	    action: string;
	    phase: string;
	    updated_at: string;
	    installed_version: string;
	    candidate_version: string;
	    is_success?: boolean;
	    failure_code: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlMaintenanceResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation_id = source["operation_id"];
	        this.action = source["action"];
	        this.phase = source["phase"];
	        this.updated_at = source["updated_at"];
	        this.installed_version = source["installed_version"];
	        this.candidate_version = source["candidate_version"];
	        this.is_success = source["is_success"];
	        this.failure_code = source["failure_code"];
	    }
	}
	export class RemoteControlMaintenance {
	    id: string;
	    action: string;
	    reason: string;
	    phase: string;
	    deadline: string;
	    is_active: boolean;
	    is_admission_blocked: boolean;
	    delivered: number;
	    is_restart_supported: boolean;
	    is_update_supported: boolean;
	    requested_operation_id: string;
	    result?: RemoteControlMaintenanceResult;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlMaintenance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.action = source["action"];
	        this.reason = source["reason"];
	        this.phase = source["phase"];
	        this.deadline = source["deadline"];
	        this.is_active = source["is_active"];
	        this.is_admission_blocked = source["is_admission_blocked"];
	        this.delivered = source["delivered"];
	        this.is_restart_supported = source["is_restart_supported"];
	        this.is_update_supported = source["is_update_supported"];
	        this.requested_operation_id = source["requested_operation_id"];
	        this.result = this.convertValues(source["result"], RemoteControlMaintenanceResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteControlMaintenanceRequest {
	    action: string;
	    warning_minutes: number;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlMaintenanceRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.warning_minutes = source["warning_minutes"];
	        this.reason = source["reason"];
	    }
	}
	
	
	export class remoteControlServer {
	    name: string;
	    description: string;
	    welcome: string;
	    rules: string;
	    contact_url: string;
	    health_status: string;
	    maintenance: RemoteControlMaintenance;
	    version: string;
	    started_at: string;
	    online_account_count: number;
	    supported_operations: string[];
	
	    static createFrom(source: any = {}) {
	        return new remoteControlServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.welcome = source["welcome"];
	        this.rules = source["rules"];
	        this.contact_url = source["contact_url"];
	        this.health_status = source["health_status"];
	        this.maintenance = this.convertValues(source["maintenance"], RemoteControlMaintenance);
	        this.version = source["version"];
	        this.started_at = source["started_at"];
	        this.online_account_count = source["online_account_count"];
	        this.supported_operations = source["supported_operations"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class remoteControlCapabilities {
	    is_admin: boolean;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlCapabilities(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.is_admin = source["is_admin"];
	    }
	}
	export class remoteControlProfile {
	    login_name: string;
	    display_name: string;
	    avatar_id: number;
	
	    static createFrom(source: any = {}) {
	        return new remoteControlProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.login_name = source["login_name"];
	        this.display_name = source["display_name"];
	        this.avatar_id = source["avatar_id"];
	    }
	}
	export class RemoteControlPanel {
	    account_id: number;
	    server_address: string;
	    expires_at: string;
	    profile: remoteControlProfile;
	    capabilities: remoteControlCapabilities;
	    server: remoteControlServer;
	    announcements: RemoteControlAnnouncement[];
	    requested_maintenance_id: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlPanel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account_id = source["account_id"];
	        this.server_address = source["server_address"];
	        this.expires_at = source["expires_at"];
	        this.profile = this.convertValues(source["profile"], remoteControlProfile);
	        this.capabilities = this.convertValues(source["capabilities"], remoteControlCapabilities);
	        this.server = this.convertValues(source["server"], remoteControlServer);
	        this.announcements = this.convertValues(source["announcements"], RemoteControlAnnouncement);
	        this.requested_maintenance_id = source["requested_maintenance_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteControlPersonalDay {
	    at: string;
	    observed_at: string;
	    level: number;
	    progression: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlPersonalDay(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.observed_at = source["observed_at"];
	        this.level = source["level"];
	        this.progression = source["progression"];
	    }
	}
	export class RemoteControlPersonalPVP {
	    at: string;
	    match_count: number;
	    playtime_seconds: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlPersonalPVP(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.match_count = source["match_count"];
	        this.playtime_seconds = source["playtime_seconds"];
	    }
	}
	export class RemoteControlPersonalPresence {
	    at: string;
	    observed_hour_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlPersonalPresence(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.observed_hour_count = source["observed_hour_count"];
	    }
	}
	export class RemoteControlPersonalActivity {
	    mission_started_at: string;
	    mission_samples: RemoteControlMissionSample[];
	    campaign_level_started_at: string;
	    campaign_levels: RemoteControlCampaignLevel[];
	    campaign_started_at: string;
	    campaign_samples: RemoteControlCampaignSample[];
	    user_id: number;
	    range: string;
	    observed_at: string;
	    range_start: string;
	    next_refresh_at: string;
	    daily_observed_at: string;
	    collection_started_at: string;
	    is_partial: boolean;
	    dna_earned: number;
	    dna_samples: RemoteControlDNASample[];
	    observed_hour_count: number;
	    presence_samples: RemoteControlPersonalPresence[];
	    pvp_samples: RemoteControlPersonalPVP[];
	    days: RemoteControlPersonalDay[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlPersonalActivity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mission_started_at = source["mission_started_at"];
	        this.mission_samples = this.convertValues(source["mission_samples"], RemoteControlMissionSample);
	        this.campaign_level_started_at = source["campaign_level_started_at"];
	        this.campaign_levels = this.convertValues(source["campaign_levels"], RemoteControlCampaignLevel);
	        this.campaign_started_at = source["campaign_started_at"];
	        this.campaign_samples = this.convertValues(source["campaign_samples"], RemoteControlCampaignSample);
	        this.user_id = source["user_id"];
	        this.range = source["range"];
	        this.observed_at = source["observed_at"];
	        this.range_start = source["range_start"];
	        this.next_refresh_at = source["next_refresh_at"];
	        this.daily_observed_at = source["daily_observed_at"];
	        this.collection_started_at = source["collection_started_at"];
	        this.is_partial = source["is_partial"];
	        this.dna_earned = source["dna_earned"];
	        this.dna_samples = this.convertValues(source["dna_samples"], RemoteControlDNASample);
	        this.observed_hour_count = source["observed_hour_count"];
	        this.presence_samples = this.convertValues(source["presence_samples"], RemoteControlPersonalPresence);
	        this.pvp_samples = this.convertValues(source["pvp_samples"], RemoteControlPersonalPVP);
	        this.days = this.convertValues(source["days"], RemoteControlPersonalDay);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	export class RemoteControlSessionInfo {
	    id: string;
	    created_at: string;
	    last_used_at: string;
	    expires_at: string;
	    is_current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.created_at = source["created_at"];
	        this.last_used_at = source["last_used_at"];
	        this.expires_at = source["expires_at"];
	        this.is_current = source["is_current"];
	    }
	}
	export class RemoteControlSessionList {
	    sessions: RemoteControlSessionInfo[];
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlSessionList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessions = this.convertValues(source["sessions"], RemoteControlSessionInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteControlSessionResult {
	    is_current: boolean;
	    revoked_count: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlSessionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.is_current = source["is_current"];
	        this.revoked_count = source["revoked_count"];
	    }
	}
	export class RemoteControlUpdate {
	    installed_version: string;
	    installed_build_id: string;
	    bootstrap_version: string;
	    bootstrap_build_id: string;
	    channel: string;
	    check_status: string;
	    checked_at?: string;
	    candidate_version: string;
	    candidate_build_id: string;
	    failure_code: string;
	    is_check_supported: boolean;
	    is_check_in_progress: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed_version = source["installed_version"];
	        this.installed_build_id = source["installed_build_id"];
	        this.bootstrap_version = source["bootstrap_version"];
	        this.bootstrap_build_id = source["bootstrap_build_id"];
	        this.channel = source["channel"];
	        this.check_status = source["check_status"];
	        this.checked_at = source["checked_at"];
	        this.candidate_version = source["candidate_version"];
	        this.candidate_build_id = source["candidate_build_id"];
	        this.failure_code = source["failure_code"];
	        this.is_check_supported = source["is_check_supported"];
	        this.is_check_in_progress = source["is_check_in_progress"];
	    }
	}
	export class RemoteControlUser {
	    ip_limit: string;
	    id: number;
	    login_name: string;
	    display_name: string;
	    level: number;
	    status: number;
	    is_banned: boolean;
	    is_timed_out: boolean;
	    timeout_until_unix: number;
	    is_online: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.login_name = source["login_name"];
	        this.display_name = source["display_name"];
	        this.level = source["level"];
	        this.status = source["status"];
	        this.ip_limit = source["ip_limit"];
	        this.is_banned = source["is_banned"];
	        this.is_timed_out = source["is_timed_out"];
	        this.timeout_until_unix = source["timeout_until_unix"];
	        this.is_online = source["is_online"];
	    }
	}
	export class RemoteControlUserList {
	    users: RemoteControlUser[];
	    next_user_id: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteControlUserList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.users = this.convertValues(source["users"], RemoteControlUser);
	        this.next_user_id = source["next_user_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RemoteProfile {
	    serverAddress: string;
	    loginName: string;
	    displayName: string;
	    createDt: string;
	    lastConnectedDt: string;
	    serverVersion: string;
	    gameVersion: string;
	    lastCheckedDt: string;
	    lastOnlineDt: string;
	    lastProfileRefreshDt: string;
	    avatarId: number;
	    avatarUrl: string;
	    crogenitorLevel: number;
	    cumulativeXp: number;
	    highestCampaignUnlocked: number;
	    isPasswordRemembered: boolean;
	    isServerOnline: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverAddress = source["serverAddress"];
	        this.loginName = source["loginName"];
	        this.displayName = source["displayName"];
	        this.createDt = source["createDt"];
	        this.lastConnectedDt = source["lastConnectedDt"];
	        this.serverVersion = source["serverVersion"];
	        this.gameVersion = source["gameVersion"];
	        this.lastCheckedDt = source["lastCheckedDt"];
	        this.lastOnlineDt = source["lastOnlineDt"];
	        this.lastProfileRefreshDt = source["lastProfileRefreshDt"];
	        this.avatarId = source["avatarId"];
	        this.avatarUrl = source["avatarUrl"];
	        this.crogenitorLevel = source["crogenitorLevel"];
	        this.cumulativeXp = source["cumulativeXp"];
	        this.highestCampaignUnlocked = source["highestCampaignUnlocked"];
	        this.isPasswordRemembered = source["isPasswordRemembered"];
	        this.isServerOnline = source["isServerOnline"];
	    }
	}
	export class RemoteServer {
	    address: string;
	    serverVersion: string;
	    gameVersion: string;
	    isMaintenance: boolean;
	    maintenanceReason: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.serverVersion = source["serverVersion"];
	        this.gameVersion = source["gameVersion"];
	        this.isMaintenance = source["isMaintenance"];
	        this.maintenanceReason = source["maintenanceReason"];
	    }
	}
	export class RemoteServerStatus {
	    address: string;
	    isOnline: boolean;
	    isMaintenance: boolean;
	    maintenanceReason: string;
	    serverVersion: string;
	    clientVersion: string;
	    isVersionMatched: boolean;
	    error: string;
	    checkedDt: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.isOnline = source["isOnline"];
	        this.isMaintenance = source["isMaintenance"];
	        this.maintenanceReason = source["maintenanceReason"];
	        this.serverVersion = source["serverVersion"];
	        this.clientVersion = source["clientVersion"];
	        this.isVersionMatched = source["isVersionMatched"];
	        this.error = source["error"];
	        this.checkedDt = source["checkedDt"];
	    }
	}
	
	export class ServerConfiguration {
	    bindHost: string;
	    advertisedHost: string;
	    isDedicated: boolean;
	    port: number;
	    isMultiplayerEnabled: boolean;
	    locale: string;
	    locales: ClientLocale[];
	    snapshotMode: string;
	    campaignMatchmakingBotSize: number;
	    isCampaignBotSquadMirroringEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bindHost = source["bindHost"];
	        this.advertisedHost = source["advertisedHost"];
	        this.isDedicated = source["isDedicated"];
	        this.port = source["port"];
	        this.isMultiplayerEnabled = source["isMultiplayerEnabled"];
	        this.locale = source["locale"];
	        this.locales = this.convertValues(source["locales"], ClientLocale);
	        this.snapshotMode = source["snapshotMode"];
	        this.campaignMatchmakingBotSize = source["campaignMatchmakingBotSize"];
	        this.isCampaignBotSquadMirroringEnabled = source["isCampaignBotSquadMirroringEnabled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServerIdentityConfiguration {
	    revision: string;
	    name: string;
	    description: string;
	    welcome: string;
	    rules: string;
	    contact_url: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerIdentityConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.welcome = source["welcome"];
	        this.rules = source["rules"];
	        this.contact_url = source["contact_url"];
	    }
	}
	export class SnapshotCompactResult {
	    names: string[];
	    directory: string;
	    snapshotCount: number;
	    removedCount: number;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotCompactResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.names = source["names"];
	        this.directory = source["directory"];
	        this.snapshotCount = source["snapshotCount"];
	        this.removedCount = source["removedCount"];
	        this.warning = source["warning"];
	    }
	}
	export class WineRunner {
	    path: string;
	    kind: string;
	    label: string;
	    version: string;
	    prefixPath: string;
	    isMissing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WineRunner(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.version = source["version"];
	        this.prefixPath = source["prefixPath"];
	        this.isMissing = source["isMissing"];
	    }
	}
	export class WineRunnerConfiguration {
	    isSupported: boolean;
	    selectedPath: string;
	    runners: WineRunner[];
	
	    static createFrom(source: any = {}) {
	        return new WineRunnerConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isSupported = source["isSupported"];
	        this.selectedPath = source["selectedPath"];
	        this.runners = this.convertValues(source["runners"], WineRunner);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	
	
	export class squadShareAbility {
	    slot: string;
	    id: number;
	
	    static createFrom(source: any = {}) {
	        return new squadShareAbility(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slot = source["slot"];
	        this.id = source["id"];
	    }
	}
	export class squadShareAbilityStat {
	    key: string;
	    token: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new squadShareAbilityStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.token = source["token"];
	        this.value = source["value"];
	    }
	}
	export class squadShareAppearance {
	    version: number;
	    isCustom: boolean;
	    previewPngBase64?: string;
	    isPreviewAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new squadShareAppearance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.isCustom = source["isCustom"];
	        this.previewPngBase64 = source["previewPngBase64"];
	        this.isPreviewAvailable = source["isPreviewAvailable"];
	    }
	}
	export class squadSharePart {
	    id: number;
	    referenceId: number;
	    level: number;
	    rarity: number;
	    isFlair: boolean;
	    rigblockAssetId: number;
	    prefixAssetId: number;
	    prefixSecondaryAssetId: number;
	    suffixAssetId: number;
	    status: number;
	    usage: number;
	
	    static createFrom(source: any = {}) {
	        return new squadSharePart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.referenceId = source["referenceId"];
	        this.level = source["level"];
	        this.rarity = source["rarity"];
	        this.isFlair = source["isFlair"];
	        this.rigblockAssetId = source["rigblockAssetId"];
	        this.prefixAssetId = source["prefixAssetId"];
	        this.prefixSecondaryAssetId = source["prefixSecondaryAssetId"];
	        this.suffixAssetId = source["suffixAssetId"];
	        this.status = source["status"];
	        this.usage = source["usage"];
	    }
	}
	export class squadShareStat {
	    name: string;
	    maximum: number;
	    current: number;
	
	    static createFrom(source: any = {}) {
	        return new squadShareStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.maximum = source["maximum"];
	        this.current = source["current"];
	    }
	}
	export class squadShareCreature {
	    creatureId: number;
	    templateName: string;
	    templateNoun: number;
	    isTemplateResolved: boolean;
	    elementType?: string;
	    classType?: string;
	    version: number;
	    gearScore: number;
	    itemPoints: number;
	    abilities: squadShareAbility[];
	    stats: squadShareStat[];
	    abilityStats: squadShareAbilityStat[];
	    equipment: squadSharePart[];
	    appearance: squadShareAppearance;
	
	    static createFrom(source: any = {}) {
	        return new squadShareCreature(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.creatureId = source["creatureId"];
	        this.templateName = source["templateName"];
	        this.templateNoun = source["templateNoun"];
	        this.isTemplateResolved = source["isTemplateResolved"];
	        this.elementType = source["elementType"];
	        this.classType = source["classType"];
	        this.version = source["version"];
	        this.gearScore = source["gearScore"];
	        this.itemPoints = source["itemPoints"];
	        this.abilities = this.convertValues(source["abilities"], squadShareAbility);
	        this.stats = this.convertValues(source["stats"], squadShareStat);
	        this.abilityStats = this.convertValues(source["abilityStats"], squadShareAbilityStat);
	        this.equipment = this.convertValues(source["equipment"], squadSharePart);
	        this.appearance = this.convertValues(source["appearance"], squadShareAppearance);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class squadShareSquad {
	    name: string;
	    category: string;
	    slot: number;
	    isLocked: boolean;
	    members: squadShareCreature[];
	
	    static createFrom(source: any = {}) {
	        return new squadShareSquad(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.category = source["category"];
	        this.slot = source["slot"];
	        this.isLocked = source["isLocked"];
	        this.members = this.convertValues(source["members"], squadShareCreature);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class squadShareUnlocks {
	    isAllAccessGranted: boolean;
	    isOnlineAccessGranted: boolean;
	    isOverdriveUnlocked: boolean;
	    catalysts: number;
	    diagonalCatalysts: number;
	    inventory: number;
	    fuelTanks: number;
	    pveDecks: number;
	    pvpDecks: number;
	    stats: number;
	    inventoryIdentify: number;
	    editorFlairSlots: number;
	
	    static createFrom(source: any = {}) {
	        return new squadShareUnlocks(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isAllAccessGranted = source["isAllAccessGranted"];
	        this.isOnlineAccessGranted = source["isOnlineAccessGranted"];
	        this.isOverdriveUnlocked = source["isOverdriveUnlocked"];
	        this.catalysts = source["catalysts"];
	        this.diagonalCatalysts = source["diagonalCatalysts"];
	        this.inventory = source["inventory"];
	        this.fuelTanks = source["fuelTanks"];
	        this.pveDecks = source["pveDecks"];
	        this.pvpDecks = source["pvpDecks"];
	        this.stats = source["stats"];
	        this.inventoryIdentify = source["inventoryIdentify"];
	        this.editorFlairSlots = source["editorFlairSlots"];
	    }
	}
	export class squadShareCrogenitor {
	    displayName: string;
	    level: number;
	    experience: number;
	    campaignProgression: number;
	    highestCampaignUnlocked: number;
	    defaultPveDeckId: number;
	    defaultPvpDeckId: number;
	    isTutorialCompleted: boolean;
	    isTutorialCompletionPending: boolean;
	    unlocks: squadShareUnlocks;
	    squads: squadShareSquad[];
	
	    static createFrom(source: any = {}) {
	        return new squadShareCrogenitor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.level = source["level"];
	        this.experience = source["experience"];
	        this.campaignProgression = source["campaignProgression"];
	        this.highestCampaignUnlocked = source["highestCampaignUnlocked"];
	        this.defaultPveDeckId = source["defaultPveDeckId"];
	        this.defaultPvpDeckId = source["defaultPvpDeckId"];
	        this.isTutorialCompleted = source["isTutorialCompleted"];
	        this.isTutorialCompletionPending = source["isTutorialCompletionPending"];
	        this.unlocks = this.convertValues(source["unlocks"], squadShareUnlocks);
	        this.squads = this.convertValues(source["squads"], squadShareSquad);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class squadShareSnapshot {
	    formatVersion: number;
	    generatedAt: string;
	    crogenitors: squadShareCrogenitor[];
	    appearanceNote: string;
	
	    static createFrom(source: any = {}) {
	        return new squadShareSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.formatVersion = source["formatVersion"];
	        this.generatedAt = source["generatedAt"];
	        this.crogenitors = this.convertValues(source["crogenitors"], squadShareCrogenitor);
	        this.appearanceNote = source["appearanceNote"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	

}

export namespace runtime {
	
	export class AllyAlertConfiguration {
	    version: number;
	    isEnabled: boolean;
	    rangePercent: number;
	    rangeOwner: string;
	    maxHops: number;
	    isDiagnosticLoggingEnabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AllyAlertConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.isEnabled = source["isEnabled"];
	        this.rangePercent = source["rangePercent"];
	        this.rangeOwner = source["rangeOwner"];
	        this.maxHops = source["maxHops"];
	        this.isDiagnosticLoggingEnabled = source["isDiagnosticLoggingEnabled"];
	    }
	}
	export class BossSpawningConfiguration {
	    leaderSelection: string;
	    replayLeaderSelection: string;
	    version: number;
	    openingAddsPerAnchor: number;
	    initialBossAddCount: number;
	    captainDelayMillisecond: number;
	
	    static createFrom(source: any = {}) {
	        return new BossSpawningConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.leaderSelection = source["leaderSelection"];
	        this.replayLeaderSelection = source["replayLeaderSelection"];
	        this.version = source["version"];
	        this.openingAddsPerAnchor = source["openingAddsPerAnchor"];
	        this.initialBossAddCount = source["initialBossAddCount"];
	        this.captainDelayMillisecond = source["captainDelayMillisecond"];
	    }
	}
	export class EnemyPopulationConfiguration {
	    version: number;
	    enemiesPerSection: number;
	    agentSelection: string;
	    agentsPerSection: number;
	    rosterSelection: string;
	    externalRosterWeightPercent: number;
	    isAmbientEliteEnabled: boolean;
	    ambientEliteChancePercent: number;
	    ownedSummonHealthScaling: string;
	    fixtureHealthScaling: string;
	
	    static createFrom(source: any = {}) {
	        return new EnemyPopulationConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.enemiesPerSection = source["enemiesPerSection"];
	        this.agentSelection = source["agentSelection"];
	        this.agentsPerSection = source["agentsPerSection"];
	        this.rosterSelection = source["rosterSelection"];
	        this.externalRosterWeightPercent = source["externalRosterWeightPercent"];
	        this.isAmbientEliteEnabled = source["isAmbientEliteEnabled"];
	        this.ambientEliteChancePercent = source["ambientEliteChancePercent"];
	        this.ownedSummonHealthScaling = source["ownedSummonHealthScaling"];
	        this.fixtureHealthScaling = source["fixtureHealthScaling"];
	    }
	}
	export class MutationAgentConfiguration {
	    version: number;
	    isEnabled: boolean;
	    minimumChainLevel: number;
	    waveCount: number;
	    spawnWave: number;
	    interWaveDelayMillisecond: number;
	    isAgentDefeatCompleting: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MutationAgentConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.isEnabled = source["isEnabled"];
	        this.minimumChainLevel = source["minimumChainLevel"];
	        this.waveCount = source["waveCount"];
	        this.spawnWave = source["spawnWave"];
	        this.interWaveDelayMillisecond = source["interWaveDelayMillisecond"];
	        this.isAgentDefeatCompleting = source["isAgentDefeatCompleting"];
	    }
	}
	export class ServerRulesConfiguration {
	    enemyPopulation: EnemyPopulationConfiguration;
	    allyAlert: AllyAlertConfiguration;
	    bossSpawning: BossSpawningConfiguration;
	    mutationAgent: MutationAgentConfiguration;
	
	    static createFrom(source: any = {}) {
	        return new ServerRulesConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enemyPopulation = this.convertValues(source["enemyPopulation"], EnemyPopulationConfiguration);
	        this.allyAlert = this.convertValues(source["allyAlert"], AllyAlertConfiguration);
	        this.bossSpawning = this.convertValues(source["bossSpawning"], BossSpawningConfiguration);
	        this.mutationAgent = this.convertValues(source["mutationAgent"], MutationAgentConfiguration);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

