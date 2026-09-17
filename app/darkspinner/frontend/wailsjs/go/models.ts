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
	    launcherNotice: string;
	    manifestUrl: string;
	    gameDirectory: string;
	    version: string;
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
	        this.launcherNotice = source["launcherNotice"];
	        this.manifestUrl = source["manifestUrl"];
	        this.gameDirectory = source["gameDirectory"];
	        this.version = source["version"];
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
	        this.isLastRunFailure = source["isLastRunFailure"];
	        this.isStartupBlocked = source["isStartupBlocked"];
	    }
	}
	export class Profile {
	    loginName: string;
	    displayName: string;
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
	    avatarId: number;
	    avatarUrl: string;
	    crogenitorLevel: number;
	    cumulativeXp: number;
	    highestCampaignUnlocked: number;
	    isPasswordRemembered: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverAddress = source["serverAddress"];
	        this.loginName = source["loginName"];
	        this.displayName = source["displayName"];
	        this.avatarId = source["avatarId"];
	        this.avatarUrl = source["avatarUrl"];
	        this.crogenitorLevel = source["crogenitorLevel"];
	        this.cumulativeXp = source["cumulativeXp"];
	        this.highestCampaignUnlocked = source["highestCampaignUnlocked"];
	        this.isPasswordRemembered = source["isPasswordRemembered"];
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
	export class ReportResult {
	    name: string;
	    directory: string;
	    fileCount: number;
	
	    static createFrom(source: any = {}) {
	        return new ReportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.directory = source["directory"];
	        this.fileCount = source["fileCount"];
	    }
	}
	export class ServerConfiguration {
	    port: number;
	    isMultiplayerEnabled: boolean;
	    locale: string;
	    locales: ClientLocale[];
	    snapshotMode: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfiguration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.isMultiplayerEnabled = source["isMultiplayerEnabled"];
	        this.locale = source["locale"];
	        this.locales = this.convertValues(source["locales"], ClientLocale);
	        this.snapshotMode = source["snapshotMode"];
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

