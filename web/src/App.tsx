import {useState} from 'react';
import {Session} from './session';
import {secureOrigin} from './api';
export function App(){
 const [session]=useState(()=>new Session());const [connected,setConnected]=useState(false);const[token,setToken]=useState('');const[error,setError]=useState('');
 return <main><h1>Jev Router</h1>{connected?<button onClick={()=>{session.disconnect();setConnected(false)}}>Disconnect</button>:<form onSubmit={async e=>{e.preventDefault();if(!secureOrigin(new URL(location.href))){setError('HTTPS required');return}const value=token;setToken('');try{setConnected(await session.connect(value))}catch{setError('Unable to connect')}}}><label>Settings credential<input type="password" value={token} onChange={e=>setToken(e.target.value)}/></label><button>Connect</button><p role="alert">{error}</p></form>}</main>
}
