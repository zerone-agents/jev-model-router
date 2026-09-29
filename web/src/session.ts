import {createManagementClient,type Client,type Capability} from './api';
export class Session{
 client:Client|null=null;capabilities:Capability[]=[];private generation=0;private pending:Client|null=null;
 constructor(private fetcher:typeof fetch=fetch){}
 async connect(token:string){
  this.disconnect();const generation=this.generation;const client=createManagementClient(token,this.fetcher);this.pending=client;
  try{const result=await client.schema();if(generation!==this.generation){client.dispose();return false}
   this.client=client;this.pending=null;this.capabilities=result.data.filter(c=>c.availability==='available');return true;
  }catch(e){client.dispose();if(generation!==this.generation)return false;this.pending=null;throw e}
 }
 disconnect(){this.generation++;this.client?.dispose();this.pending?.dispose();this.client=null;this.pending=null;this.capabilities=[]}
 expire(client:Client){if(client===this.client)this.disconnect()}
}
