export namespace main {
	
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
	export class LauncherIntegrationStatus {
	    isStartMenuInstalled: boolean;
	    isDesktopInstalled: boolean;
	    isSteamLaunchInstalled: boolean;
	    isManagementSupported: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LauncherIntegrationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isStartMenuInstalled = source["isStartMenuInstalled"];
	        this.isDesktopInstalled = source["isDesktopInstalled"];
	        this.isSteamLaunchInstalled = source["isSteamLaunchInstalled"];
	        this.isManagementSupported = source["isManagementSupported"];
	        this.message = source["message"];
	    }
	}
	export class LauncherStatus {
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
	
	    static createFrom(source: any = {}) {
	        return new RemoteServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.serverVersion = source["serverVersion"];
	        this.gameVersion = source["gameVersion"];
	    }
	}
	export class RemoteServerStatus {
	    address: string;
	    isOnline: boolean;
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
	        this.serverVersion = source["serverVersion"];
	        this.clientVersion = source["clientVersion"];
	        this.isVersionMatched = source["isVersionMatched"];
	        this.error = source["error"];
	        this.checkedDt = source["checkedDt"];
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
	export class ServerConfiguration {
	    bindHost: string;
	    advertisedHost: string;
	    isDedicated: boolean;
	    port: number;
	    isMultiplayerEnabled: boolean;
	    locale: string;
	    nativeUIScalePercent: number;
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
	        this.nativeUIScalePercent = source["nativeUIScalePercent"];
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

