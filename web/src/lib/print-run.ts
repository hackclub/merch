import type { PrintRequest } from './print';
import type { SwagRequest } from './types';
export const stages=['submitted','approved','ordered','in_transit','received','available'];
export const stageLabel:Record<string,string>={submitted:'Submitted',approved:'Approved',ordered:'Ordered from printer',in_transit:'In transit',received:'Received at warehouse',available:'Available',rejected:'Rejected',cancelled:'Cancelled'};
export type PO={po_id:number;line_id:number;sku:string;order_number:string;supplier:string;quantity:number;draft:boolean;deleted:boolean;completed:boolean;created_at:string|null;required_by:string|null;completed_at:string|null;synced_at:string;polled_at:string};
export type PrintRun={id:number;user_id:number;user_name?:string;user_email?:string;origin:string;stage:string;version:number;request:PrintRequest&{front_preview_url?:string;back_preview_url?:string};approved_quantity:number;approved_at:string|null;approved_by:number;specifications:string;revision:number;sku:string;item_id:number;public_note:string;internal_note?:string;tracking:string;eta:string;eta_source:string;dispatched_at:string|null;reconcile:boolean;legacy_review:boolean;legacy_status:string;slack_opt_in:boolean;parent_id:number;last_poll_at:string|null;poll_error:string;shipment_request_id:number|null;created_at:string;links:PO[]};
export type PrintEvent={id:number;stage:string;action:string;actor_id:number|null;at:string|null;recorded_at:string;note:string;source:string;version:number};
export type Notice={id:number;channel:string;subject:string;body:string;status:string;attempts:number;error?:string};
export type PrintDetail={run:PrintRun;history:PrintEvent[];notifications:Notice[];stock:{synced_at:string|null;warehouse_sellable:number|null;held:number|null;available:number|null;inbound:number|null;visible:boolean};shipment:SwagRequest|null;po_automation:boolean};
export function nextOwner(r:Pick<PrintRun,'stage'|'legacy_review'|'reconcile'>):string{
 if(r.legacy_review||r.reconcile)return 'HQ: reconcile warehouse evidence';
 return ({submitted:'HQ: review artwork and quantity',approved:'HQ: link printer order and PO',ordered:'HQ: record printer dispatch',in_transit:'Warehouse: complete linked PO',received:'HQ: release listing',available:'Shipment review / requester postage',rejected:'Requester: revise and resubmit',cancelled:'Closed'} as Record<string,string>)[r.stage]??'HQ: review';
}
export function poState(p:PO,now=Date.now()):string{
 if(!p.synced_at||now-new Date(p.synced_at).getTime()>3600000)return 'Not yet confirmed in latest copy';
 if(p.draft||p.deleted)return 'Needs HQ review: draft or deleted PO';
 return p.completed?'PO complete (whole order)':'Received quantity unavailable';
}
export function actionPreview(r:PrintRun,action:string,quantity:number,note:string,eta:string):string{
 const item=r.request.title;
 switch(action){
 case 'approve':return `Your ${item} request is approved for ${quantity}. HQ will place the printer order next.`;
 case 'reject':return `Your ${item} request was rejected: ${note}. No order was placed.`;
 case 'cancel':return `Your ${item} print request was cancelled. ${note}. ${['ordered','in_transit'].includes(r.stage)?'Printer confirmed cancellation.':'No order was placed.'}`;
 case 'dispatch':return `Your print run has shipped from the printer to the warehouse. Estimated arrival: ${eta||'not confirmed'}. This is not your first parcel.`;
 case 'release':return `${item} is available in the shop. Your first ${r.request.initial_quantity} will be linked to a shipment request, awaiting review or postage payment. Your first parcel has not shipped.`;
 case 'update':return note;
 default:return 'This saves a timeline event. No stage email is sent until the stage changes.';
 }
}
