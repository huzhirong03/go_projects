export namespace service {
	
	export class AdvancedConditionDTO {
	    column: string;
	    op: string;
	    value: string;
	    value2?: string;
	    format?: string;
	
	    static createFrom(source: any = {}) {
	        return new AdvancedConditionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column = source["column"];
	        this.op = source["op"];
	        this.value = source["value"];
	        this.value2 = source["value2"];
	        this.format = source["format"];
	    }
	}
	export class AdvancedFilterDTO {
	    mode: string;
	    conditions: AdvancedConditionDTO[];
	
	    static createFrom(source: any = {}) {
	        return new AdvancedFilterDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.conditions = this.convertValues(source["conditions"], AdvancedConditionDTO);
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
	    advancedFilter?: AdvancedFilterDTO;
	
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
	        this.advancedFilter = source["advancedFilter"] ? new AdvancedFilterDTO(source["advancedFilter"]) : undefined;
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
	    advancedFilter?: AdvancedFilterDTO;
	
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
	        this.advancedFilter = source["advancedFilter"] ? new AdvancedFilterDTO(source["advancedFilter"]) : undefined;
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

