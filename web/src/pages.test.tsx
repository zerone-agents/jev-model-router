import {it,expect,vi} from 'vitest';
import {render,screen,waitFor} from '@testing-library/react';
import {Overview,Records,Models} from './pages';
import {createManagementClient} from './api';
const client=(data:unknown)=>createManagementClient('x',vi.fn().mockResolvedValue(new Response(JSON.stringify({ok:true,data,meta:{}}))));
it('shows readiness separately from actual upstream health',async()=>{
 render(<Overview client={client({ready:true,version:3,enabled_models:2,records_degraded:true})} lang="en" onError={()=>{}}/>);
 expect(await screen.findByText('Ready')).toBeInTheDocument();expect(screen.getByText(/not an upstream health check/)).toBeInTheDocument();expect(screen.getByText(/degraded/i)).toBeInTheDocument();
});
it('empty records do not fabricate metrics',async()=>{render(<Records client={client({records:[],next_cursor:''})} lang="en" onError={()=>{}}/>);expect(await screen.findByText('No routing records yet')).toBeInTheDocument()});
it('model descriptions are rendered as text',async()=>{render(<Models client={client({version:1,items:[{id:'m',description:'<script>evil()</script>',enabled:true,capabilities:{}}],next_cursor:''})} lang="en" onError={()=>{}} canEdit={false} onDirty={()=>{}}/>);await waitFor(()=>expect(screen.getByText('<script>evil()</script>')).toBeInTheDocument());expect(document.querySelector('script')).toBeNull()});
