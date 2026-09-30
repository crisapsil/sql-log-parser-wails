export namespace main {
	
	export class ParseResult {
	    rows: string;
	    count: number;
	    isMerge: boolean;
	    rawContent?: string;
	    logName?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ParseResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = source["rows"];
	        this.count = source["count"];
	        this.isMerge = source["isMerge"];
	        this.rawContent = source["rawContent"];
	        this.logName = source["logName"];
	        this.error = source["error"];
	    }
	}

}

