import{a as E}from"./chunk-ITUF2PA2.js";import{a as F}from"./chunk-QL5PROXB.js";import{a as z}from"./chunk-X5QHV73L.js";import"./chunk-UROR77XA.js";import"./chunk-EA7ECKFS.js";import"./chunk-MHAGJOBR.js";import"./chunk-4XB5VX2P.js";import"./chunk-FS7DBJ7U.js";import"./chunk-RFESTF5V.js";import"./chunk-DHAHX2GN.js";import"./chunk-TUHNTJTY.js";import{C as w}from"./chunk-GMIKWTZW.js";import"./chunk-C2AXNXJS.js";import{A as x,N as y,R as $,S as B,T as C,U as S,V as D,W as T,X as P,r as v}from"./chunk-YWNQD3EY.js";import{b as h,d as u}from"./chunk-45V3GG25.js";import"./chunk-GFYG2RYY.js";import"./chunk-ROEDOZOH.js";import"./chunk-DSWCP2YD.js";import"./chunk-DXSVFN6L.js";import"./chunk-6BUS7JER.js";import"./chunk-6Y2J7PYM.js";var L=v.packet,m,A=(m=class{constructor(){this.packet=[],this.setAccTitle=B,this.getAccTitle=C,this.setDiagramTitle=T,this.getDiagramTitle=P,this.getAccDescription=D,this.setAccDescription=S}getConfig(){let t=w({...L,...x().packet});return t.showBits&&(t.paddingY+=10),t}getPacket(){return this.packet}pushWord(t){t.length>0&&this.packet.push(t)}clear(){$(),this.packet=[]}},h(m,"PacketDB"),m),M=1e4,Y=h((e,t)=>{E(e,t);let a=-1,o=[],n=1,{bitsPerRow:l}=t.getConfig();for(let{start:r,end:s,bits:d,label:c}of e.blocks){if(r!==void 0&&s!==void 0&&s<r)throw new Error(`Packet block ${r} - ${s} is invalid. End must be greater than start.`);if(r??(r=a+1),r!==a+1)throw new Error(`Packet block ${r} - ${s??r} is not contiguous. It should start from ${a+1}.`);if(d===0)throw new Error(`Packet block ${r} is invalid. Cannot have a zero bit field.`);for(s??(s=r+(d??1)-1),d??(d=s-r+1),a=s,u.debug(`Packet block ${r} - ${a} with label ${c}`);o.length<=l+1&&t.getPacket().length<M;){let[p,i]=I({start:r,end:s,bits:d,label:c},n,l);if(o.push(p),p.end+1===n*l&&(t.pushWord(o),o=[],n++),!i)break;({start:r,end:s,bits:d,label:c}=i)}}t.pushWord(o)},"populate"),I=h((e,t,a)=>{if(e.start===void 0)throw new Error("start should have been set during first phase");if(e.end===void 0)throw new Error("end should have been set during first phase");if(e.start>e.end)throw new Error(`Block start ${e.start} is greater than block end ${e.end}.`);if(e.end+1<=t*a)return[e,void 0];let o=t*a-1,n=t*a;return[{start:e.start,end:o,label:e.label,bits:o-e.start},{start:n,end:e.end,label:e.label,bits:e.end-n}]},"getNextFittingBlock"),W={parser:{yy:void 0},parse:h(async e=>{let t=await F("packet",e),a=W.parser?.yy;if(!(a instanceof A))throw new Error("parser.parser?.yy was not a PacketDB. This is due to a bug within Mermaid, please report this issue at https://github.com/mermaid-js/mermaid/issues.");u.debug(t),Y(t,a)},"parse")},O=h((e,t,a,o)=>{let n=o.db,l=n.getConfig(),{rowHeight:r,paddingY:s,bitWidth:d,bitsPerRow:c}=l,p=n.getPacket(),i=n.getDiagramTitle(),f=r+s,g=f*(p.length+1)-(i?0:r),k=d*c+2,b=z(t);b.attr("viewBox",`0 0 ${k} ${g}`),y(b,g,k,l.useMaxWidth);for(let[_,N]of p.entries())j(b,N,_,l);b.append("text").text(i).attr("x",k/2).attr("y",g-f/2).attr("dominant-baseline","middle").attr("text-anchor","middle").attr("class","packetTitle")},"draw"),j=h((e,t,a,{rowHeight:o,paddingX:n,paddingY:l,bitWidth:r,bitsPerRow:s,showBits:d})=>{let c=e.append("g"),p=a*(o+l)+l;for(let i of t){let f=i.start%s*r+1,g=(i.end-i.start+1)*r-n;if(c.append("rect").attr("x",f).attr("y",p).attr("width",g).attr("height",o).attr("class","packetBlock"),c.append("text").attr("x",f+g/2).attr("y",p+o/2).attr("class","packetLabel").attr("dominant-baseline","middle").attr("text-anchor","middle").text(i.label),!d)continue;let k=i.end===i.start,b=p-2;c.append("text").attr("x",f+(k?g/2:0)).attr("y",b).attr("class","packetByte start").attr("dominant-baseline","auto").attr("text-anchor",k?"middle":"start").text(i.start),k||c.append("text").attr("x",f+g).attr("y",b).attr("class","packetByte end").attr("dominant-baseline","auto").attr("text-anchor","end").text(i.end)}},"drawWord"),G={draw:O},H={byteFontSize:"10px",startByteColor:"black",endByteColor:"black",labelColor:"black",labelFontSize:"12px",titleColor:"black",titleFontSize:"14px",blockStrokeColor:"black",blockStrokeWidth:"1",blockFillColor:"#efefef"},K=h(({packet:e}={})=>{let t=w(H,e);return`
	.packetByte {
		font-size: ${t.byteFontSize};
	}
	.packetByte.start {
		fill: ${t.startByteColor};
	}
	.packetByte.end {
		fill: ${t.endByteColor};
	}
	.packetLabel {
		fill: ${t.labelColor};
		font-size: ${t.labelFontSize};
	}
	.packetTitle {
		fill: ${t.titleColor};
		font-size: ${t.titleFontSize};
	}
	.packetBlock {
		stroke: ${t.blockStrokeColor};
		stroke-width: ${t.blockStrokeWidth};
		fill: ${t.blockFillColor};
	}
	`},"styles"),V={parser:W,get db(){return new A},renderer:G,styles:K};export{V as diagram};
