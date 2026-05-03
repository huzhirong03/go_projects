export namespace service {
	
	export class ExtractRequest {
	    folderPath: string;
	    keywordsRaw: string;
	    exact: boolean;
	    contains: boolean;
	    pinyin: boolean;
	    searchAllCols: boolean;
	    searchColumns: string[];
	    strategy: string;
	    outputDir: string;
	    headerRow: number;
	    preserveImages: boolean;
	    sheetNames: string[];
	    filenamePrefix: string;
	    csvEncoding: string;
	    csvDelimiter: string;
	    outputTarget: string;
	    backupSource: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExtractRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folderPath = source["folderPath"];
	        this.keywordsRaw = source["keywordsRaw"];
	        this.exact = source["exact"];
	        this.contains = source["contains"];
	        this.pinyin = source["pinyin"];
	        this.searchAllCols = source["searchAllCols"];
	        this.searchColumns = source["searchColumns"];
	        this.strategy = source["strategy"];
	        this.outputDir = source["outputDir"];
	        this.headerRow = source["headerRow"];
	        this.preserveImages = source["preserveImages"];
	        this.sheetNames = source["sheetNames"];
	        this.filenamePrefix = source["filenamePrefix"];
	        this.csvEncoding = source["csvEncoding"];
	        this.csvDelimiter = source["csvDelimiter"];
	        this.outputTarget = source["outputTarget"];
	        this.backupSource = source["backupSource"];
	    }
	}
	export class FilePreview {
	    path: string;
	    sheets: string[];
	    columns: string[];
	
	    static createFrom(source: any = {}) {
	        return new FilePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sheets = source["sheets"];
	        this.columns = source["columns"];
	    }
	}
	export class HeaderPreview {
	    firstFile: string;
	    columns: string[];
	    sheets: string[];
	
	    static createFrom(source: any = {}) {
	        return new HeaderPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.firstFile = source["firstFile"];
	        this.columns = source["columns"];
	        this.sheets = source["sheets"];
	    }
	}
	export class SplitRequest {
	    sourcePath: string;
	    mode: string;
	    rowsPerFile: number;
	    splitColumn: string;
	    outputDir: string;
	    headerRow: number;
	    preserveImages: boolean;
	    sheetNames: string[];
	    keywordsRaw: string;
	    exact: boolean;
	    contains: boolean;
	    pinyin: boolean;
	    searchAllCols: boolean;
	    searchColumns: string[];
	    strategy: string;
	    csvEncoding: string;
	    csvDelimiter: string;
	    outputTarget: string;
	    backupSource: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SplitRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.mode = source["mode"];
	        this.rowsPerFile = source["rowsPerFile"];
	        this.splitColumn = source["splitColumn"];
	        this.outputDir = source["outputDir"];
	        this.headerRow = source["headerRow"];
	        this.preserveImages = source["preserveImages"];
	        this.sheetNames = source["sheetNames"];
	        this.keywordsRaw = source["keywordsRaw"];
	        this.exact = source["exact"];
	        this.contains = source["contains"];
	        this.pinyin = source["pinyin"];
	        this.searchAllCols = source["searchAllCols"];
	        this.searchColumns = source["searchColumns"];
	        this.strategy = source["strategy"];
	        this.csvEncoding = source["csvEncoding"];
	        this.csvDelimiter = source["csvDelimiter"];
	        this.outputTarget = source["outputTarget"];
	        this.backupSource = source["backupSource"];
	    }
	}
	export class TaskHandle {
	    taskId: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskHandle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	    }
	}

}

