'use strict';
const $=id=>document.getElementById(id);
const api=async(m,u,b)=>{const r=await fetch(u,{method:m,headers:{'Content-Type':'application/json'},body:b?JSON.stringify(b):undefined});const j=await r.json().catch(()=>({}));if(!r.ok)throw new Error(j.error||r.statusText);return j;};
const human=n=>{const u=['o','Ko','Mo','Go','To'];let i=0;while(n>=1024&&i<4){n/=1024;i++}return n.toFixed(i?1:0).replace('.',',')+' '+u[i]};
const esc=s=>String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const ico=n=>`<svg class="i" aria-hidden="true"><use href="#i-${n}"/></svg>`;
const msg=(kind,text,extra='')=>`<div class="msg ${kind}">${ico(kind==='ok'?'check':kind==='err'?'x':kind==='warn'?'alert':'info')}<div>${text}${extra}</div></div>`;
const st={step:1,max:1,mode:'dir',hash:null,clientList:[],sort:{k:'name',d:1},job:null,sel:null,picked:null,analysis:null,templates:[],images:[],captures:[],me:null,miErr:'',state:null,music:false,mbPicked:null,mbResults:[],typesTouched:false};

// ---- retours à l'écran : toasts et boutons occupés ----
function toast(text,kind='info'){const t=document.createElement('div');t.className='toast '+kind;t.innerHTML=ico(kind==='ok'?'check':kind==='err'?'x':'info')+'<span>'+esc(text)+'</span>';$('toasts').append(t);setTimeout(()=>t.remove(),kind==='err'?8000:4000)}
// run : désactive le bouton le temps de la requête ; l'erreur part en toast.
async function run(btn,fn){if(btn.disabled)return;btn.disabled=true;btn.classList.add('busy');try{return await fn()}catch(e){toast(e.message,'err')}finally{btn.disabled=false;btn.classList.remove('busy')}}
const onSubmit=(form,fn)=>form.addEventListener('submit',e=>{e.preventDefault();fn()});

// ---- le rail : 4 étapes, on peut revenir sur une étape déjà atteinte ----
function go(n){st.step=n;st.max=Math.max(st.max,n);
  [...$('rail').children].forEach((li,i)=>{const k=i+1;li.className=k<n?'done reach':k===n?'on reach':k<=st.max?'reach':'';li.firstElementChild.disabled=k>st.max});
  $('railFill').style.width=((n-1)/3*100)+'%';
  [1,2,3,4].forEach(k=>$('p'+k).hidden=k!==n);scrollTop()}
// Remonter en haut APRÈS le rendu du panneau : un scrollTo lancé avant que la page ait changé de hauteur était parfois ignoré.
function scrollTop(){requestAnimationFrame(()=>{window.scrollTo({top:0,behavior:'instant'});document.documentElement.scrollTop=0;document.body.scrollTop=0})}
// Amener un élément sous l'en-tête fixe, en douceur.
function reveal(el,block='start'){if(!el)return;requestAnimationFrame(()=>{const y=el.getBoundingClientRect().top+window.scrollY-72;window.scrollTo({top:block==='start'?y:Math.max(0,y-window.innerHeight/3),behavior:'smooth'})})}
$('rail').addEventListener('click',e=>{const b=e.target.closest('button');if(b&&!b.disabled)go(+b.dataset.n)});

// ---- réglages (tiroir) ----
const drawer=$('settings');
$('gear').onclick=()=>drawer.showModal();
$('closeSettings').onclick=()=>drawer.close();
drawer.addEventListener('click',e=>{if(e.target===drawer)drawer.close()});

async function load(){const s=await api('GET','/ui/state');st.state=s;const c=s.config;
  $('version').textContent=s.version;$('site').value=c.site_url||'';$('site2').value=c.site_url||'';$('outdir').value=c.out_dir||'';
  $('mi').className='status '+(s.mediainfo?'ok':'warn');
  $('mi').innerHTML=s.mediainfo?ico('check')+'<span>mediainfo présent : les facettes seront lues dans les fichiers.</span>':ico('alert')+'<span>mediainfo absent — installe-le : <code>'+esc(s.mediainfo_hint)+'</code>. Sans lui, les facettes sont déclarées à la main et Ratatosk les vérifie.</span>';
  document.querySelector(`input[name=src][value=${c.source==='ssh'?'ssh':'local'}]`).checked=true;$('sshbox').hidden=c.source!=='ssh';
  $('cl_type').value=c.client?.type||'';$('cl_url').value=c.client?.url||'';$('cl_user').value=c.client?.user||'';$('cl_label').value=c.client?.label||'';$('cl_skip').checked=!!c.client?.skip_check;$('cl_paths').value=(c.client?.path_map||[]).map(m=>m.from+' = '+m.to).join('\n');
  $('autoUpd').checked=!!s.auto_update;
  if(s.update){$('upd').hidden=false;$('updTitle').textContent='Bifröst '+s.update.version+' est disponible (tu as '+s.version+')';
    let notes=s.update.notes||'';
    if(s.in_docker)notes+='\nEn Docker : docker pull ghcr.io/mimirdraupnirr/bifrost && docker compose up -d';
    else if(s.auto_update)notes+='\nElle s\'installera toute seule au prochain lancement.';
    else $('updBtn').hidden=false;
    $('updNotes').textContent=notes.trim()}
  if(s.locked){$('lock').hidden=false;$('gear').hidden=true;
    $('lockTitle').textContent=s.needs_password?'Définis un mot de passe local':'Mot de passe local';
    $('lockMsg').textContent=s.needs_password?'Cette page n\'écoute pas sur 127.0.0.1 : sans mot de passe, n\'importe qui sur le réseau publierait avec ton jeton. 8 caractères minimum.':'Cette page est protégée.';
    $('lockBtn').textContent=s.needs_password?'Définir':'Entrer';
    onSubmit($('lockForm'),()=>run($('lockBtn'),async()=>{try{await api('POST',s.needs_password?'/ui/password':'/ui/login',{password:$('lockPass').value});location.reload()}catch(e){$('lockErr').textContent=e.message}}));
    $('lockPass').focus();return}
  $('ssh_host').value=c.ssh?.host||'';$('ssh_port').value=c.ssh?.port||22;$('ssh_user').value=c.ssh?.user||'';$('ssh_key').value=c.ssh?.key_path||'';$('ssh_dir').value=c.ssh?.data_dir||'';
  if(!s.has_token){$('connect').hidden=false;$('acctStatus').textContent='Pas encore connecté.';$('token').focus();return}
  // Jeton enregistré : jamais redemandé, on arrive directement sur les fichiers.
  $('app').hidden=false;go(1);$('who').hidden=false;$('me').textContent='connexion…';
  if(s.connected)await connected(s.me);else{try{await connect(true)}catch(e){offline(e.message)}}}

function offline(m){$('offline').hidden=false;$('offlineMsg').textContent=m+' — le jeton enregistré n\'a pas été redemandé.';$('me').textContent='hors ligne';$('acctStatus').className='status err';$('acctStatus').innerHTML=ico('cloud-off')+'<span>'+esc(m)+'</span>'}
$('retry').onclick=()=>run($('retry'),async()=>{try{await connect(true);$('offline').hidden=true}catch(e){offline(e.message)}});
$('relog').onclick=()=>{drawer.showModal();$('token2').focus()};
async function connected(me){st.me=me;$('app').hidden=false;$('connect').hidden=true;$('who').hidden=false;
  $('me').textContent=me.name+' · '+me.class+' · ratio '+(me.ratio??'∞');
  $('acctStatus').className='status ok';$('acctStatus').innerHTML=ico('check')+'<span>Connecté : <b>'+esc(me.name)+'</b> · '+esc(me.class)+' · ratio '+esc(me.ratio??'∞')+'</span>';
  $('offline').hidden=true;await cats();if(st.mode==='client'&&st.clientList.length)renderClient();else browse('').catch(e=>listEmpty('alert',e.message))}
async function connect(silent,site,token){const r=await api('POST','/ui/connect',{site_url:site??$('site').value,token:silent?'':token});$('token').value='';$('token2').value='';await connected(r.me)}
onSubmit($('connectForm'),()=>run($('connectBtn'),async()=>{$('connectErr').textContent='';try{await connect(false,$('site').value,$('token').value);$('site2').value=$('site').value;toast('Connecté','ok')}catch(e){$('connectErr').textContent=e.message}}));
onSubmit($('acctForm'),()=>run($('connect2'),async()=>{$('acctErr').textContent='';try{await connect(false,$('site2').value,$('token2').value);$('site').value=$('site2').value;toast('Connecté avec le nouveau jeton','ok');drawer.close()}catch(e){$('acctErr').textContent=e.message}}));

onSubmit($('outForm'),()=>run($('saveOut'),async()=>{await api('POST','/ui/settings',{out_dir:$('outdir').value});toast('Dossier enregistré','ok')}));
$('autoUpd').onchange=()=>api('POST','/ui/settings',{auto_update:$('autoUpd').checked}).then(()=>toast($('autoUpd').checked?'Mises à jour automatiques activées':'Mises à jour automatiques désactivées','ok'),e=>toast(e.message,'err'));
$('updBtn').onclick=()=>run($('updBtn'),async()=>{const r=await api('POST','/ui/update');$('updTitle').textContent=r.status;$('updBtn').hidden=true;setTimeout(()=>location.reload(),4000)});

document.querySelectorAll('input[name=src]').forEach(r=>r.onchange=async()=>{$('sshbox').hidden=r.value!=='ssh';try{await api('POST','/ui/settings',{source:r.value});toast(r.value==='ssh'?'Source : la seedbox':'Source : ce poste','ok');if(st.me&&st.mode==='dir')browse('')}catch(e){toast(e.message,'err')}});
const sshCfg=()=>({host:$('ssh_host').value.trim(),port:parseInt($('ssh_port').value)||22,user:$('ssh_user').value.trim(),key_path:$('ssh_key').value.trim(),data_dir:$('ssh_dir').value.trim()});
function sshSteps(list){$('sshSteps').innerHTML=list.map(x=>`<div class="${x.ok?'ok':'err'}">${ico(x.ok?'check':'x')}<span>${esc(x.label)}${x.detail?' — '+esc(x.detail):''}</span></div>`).join('')}
$('sshSave').onclick=()=>run($('sshSave'),async()=>{await api('POST','/ui/settings',{source:'ssh',ssh:sshCfg(),password:$('ssh_pass').value});sshSteps([{ok:true,label:'Réglages enregistrés'}])});
$('sshTest').onclick=()=>run($('sshTest'),async()=>{sshSteps([{ok:true,label:'Connexion en cours…'}]);$('hostkey').hidden=true;
  try{const r=await api('POST','/ui/ssh/test',{ssh:sshCfg(),password:$('ssh_pass').value});sshSteps(r.steps);
    if(r.hostkey){$('hostkeyMsg').textContent=(r.hostkey.Changed?'La clé de cet hôte a CHANGÉ. ':'Première connexion à '+r.hostkey.Host+'. ')+'Empreinte : '+r.hostkey.Fingerprint;$('hostkey').hidden=false;
      $('hostkeyAccept').onclick=()=>run($('hostkeyAccept'),async()=>{await api('POST','/ui/ssh/accept',{key:r.hostkey.Key});$('hostkey').hidden=true;$('sshTest').click()})}
    else if(r.steps.every(x=>x.ok)){document.querySelector('input[name=src][value=ssh]').checked=true;await api('POST','/ui/settings',{source:'ssh'});if(st.me&&st.mode==='dir')browse('')}}
  catch(e){sshSteps([{ok:false,label:e.message}])}});
const clCfg=()=>({type:$('cl_type').value,url:$('cl_url').value.trim(),user:$('cl_user').value.trim(),password:$('cl_pass').value,label:$('cl_label').value.trim(),skip_check:$('cl_skip').checked,
  path_map:$('cl_paths').value.split('\n').map(l=>{const i=l.indexOf('=');return i<1?{}:{from:l.slice(0,i).trim(),to:l.slice(i+1).trim()}}).filter(m=>m.from&&m.to)});
$('clDetect').onclick=()=>run($('clDetect'),async()=>{$('clMsg').className='status';$('clMsg').textContent='Sondage des ports habituels…';
  const r=await api('GET','/ui/client/detect');const c=(r.clients||[])[0];
  if(!c){$('clMsg').className='status err';$('clMsg').textContent='Aucun client trouvé sur ce poste. Active l\'interface Web du client (voir ci-dessus), ou saisis son adresse.';return}
  $('cl_type').value=c.type;$('cl_url').value=c.url;$('clMsg').className='status ok';$('clMsg').textContent=`Trouvé : ${c.type} sur ${c.url}${r.clients.length>1?' (+'+(r.clients.length-1)+' autre(s))':''}. Renseigne identifiant et mot de passe, puis Tester.`});
$('clTest').onclick=()=>run($('clTest'),async()=>{$('clMsg').className='status';$('clMsg').textContent='Connexion…';
  try{const r=await api('POST','/ui/client/test',clCfg());$('clMsg').className='status ok';$('clMsg').innerHTML=ico('check')+'<span>'+esc(r.version)+'</span>';$('cl_pass').value=''}
  catch(e){$('clMsg').className='status err';$('clMsg').innerHTML=ico('x')+'<span>'+esc(e.message)+'</span>'}});

// ---- étape 1 : fichiers ----
function listEmpty(icon,text){$('entries').innerHTML=`<div class="empty">${ico(icon)}<span>${esc(text)}</span></div>`}
function select(el){[...$('entries').querySelectorAll('.sel')].forEach(x=>x.classList.remove('sel'));el.classList.add('sel');$('prepare').disabled=false}
async function browse(p){st.mode='dir';$('modeDir').classList.add('on');$('modeClient').classList.remove('on');$('dirBar').hidden=false;$('clientBar').hidden=true;$('clientNote').hidden=true;
  st.sel=null;st.hash=null;$('prepare').disabled=true;$('entries').className='list';listEmpty('folder','Lecture du dossier…');const seq=st.browseSeq=(st.browseSeq||0)+1;
  const r=await api('GET','/ui/browse?path='+encodeURIComponent(p));if(seq!==st.browseSeq)return; /* une lecture plus récente a pris la main */$('path').value=r.path;$('path').dataset.parent=r.parent;
  $('entries').innerHTML=r.entries.map(e=>`<div class="e ${e.is_dir?'dir':''}" data-p="${esc(e.path)}" data-d="${e.is_dir?1:0}">${ico(e.is_dir?'folder':/\.(mkv|mp4|avi|m2ts|ts|iso)$/i.test(e.name)?'film':'file')}<span class="n" title="${esc(e.path)}">${esc(e.name)}</span><span class="sz">${human(e.size)}</span>${e.is_dir?`<button type="button" class="enter ghost" title="Entrer dans ce dossier">${ico('chevron-right')}</button>`:''}</div>`).join('');
  if(!r.entries.length)listEmpty('folder','Ce dossier est vide.');
  [...$('entries').querySelectorAll('.e')].forEach(d=>{d.onclick=e=>{if(e.target.closest('.enter')){browse(d.dataset.p).catch(e=>toast(e.message,'err'));return}select(d);st.sel=d.dataset.p};d.ondblclick=()=>{if(d.dataset.d==='1')browse(d.dataset.p).catch(e=>toast(e.message,'err'))}})}
$('go').onclick=()=>run($('go'),()=>browse($('path').value));
$('path').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();$('go').click()}});
$('up').onclick=()=>run($('up'),()=>browse($('path').dataset.parent||''));
$('modeDir').onclick=()=>{if(st.mode!=='dir')browse($('path').value).catch(e=>toast(e.message,'err'))};
$('modeClient').onclick=()=>fromClient();

try{$('hideOnSite').checked=localStorage.getItem('bf.hideOnSite')!=='0';$('onlyDone').checked=localStorage.getItem('bf.onlyDone')!=='0'}catch(e){}
// En-tête de colonne triable : so = {k, d} (d = 1 ou -1), ou null.
const sortHead=(so,key,label,cls='')=>`<button type="button" data-k="${key}" class="${so&&so.k===key?'on':''} ${so&&so.k===key&&so.d<0?'desc':''}" style="${cls}">${label}${ico('sort')}</button>`;
const stateRank=t=>t.progress<1?1:/missing|error/.test((t.state||'').toLowerCase())?3:/pause|stopped/.test((t.state||'').toLowerCase())?2:0;
function badge(t){if(t.progress<1)return `<span class="bdg part">${Math.round(t.progress*100)} %</span>`;const k=stateRank(t);return k===3?'<span class="bdg dead">fichiers absents</span>':k===2?'<span class="bdg x">en pause</span>':'<span class="bdg seed">seed</span>'}
function renderClient(){const q=$('clientQ').value.toLowerCase().trim();const hide=$('hideOnSite').checked,done=$('onlyDone').checked;try{localStorage.setItem('bf.hideOnSite',hide?'1':'0');localStorage.setItem('bf.onlyDone',done?'1':'0')}catch(e){}
  const all=st.clientList||[];const{k,d}=st.sort;
  const rows=all.filter(t=>(!hide||!t.on_draupnirr)&&(!done||t.progress>=1)&&(!q||t.name.toLowerCase().includes(q)))
    .sort((a,b)=>d*(k==='size'?a.size-b.size:k==='state'?stateRank(a)-stateRank(b)||a.name.localeCompare(b.name):a.name.localeCompare(b.name)));
  const onSite=all.filter(t=>t.on_draupnirr).length;
  $('clientNote').hidden=false;$('clientNote').className='note';$('clientNote').textContent=`${rows.length} affichée${rows.length>1?'s':''} sur ${all.length}`+(onSite?` · ${onSite} déjà sur Draupnirr`:'')+' · '+st.clientHint;
  const h=(...a)=>sortHead(st.sort,...a);
  $('entries').className='list tbl';
  $('entries').innerHTML=`<div class="e h">${h('name','Release')}${h('size','Taille','justify-content:flex-end')}${h('state','État')}<span class="eyebrow" style="line-height:34px">Draupnirr</span></div>`+
    rows.map(t=>`<div class="e ${t.on_draupnirr?'dim':''}" data-h="${esc(t.hash)}"><span class="nm" title="${esc(t.path)}"><b>${esc(t.name)}</b><small>${esc(t.path)}</small></span><span class="r">${human(t.size)}</span><span>${badge(t)}${t.copies>1?` <span class="bdg x" title="mêmes données, ${t.copies} trackers">×${t.copies}</span>`:''}</span><span>${t.on_draupnirr?`<span class="bdg site">${ico('ring')}déjà dessus</span>`:'<span class="mut dash">—</span>'}</span></div>`).join('');
  if(!rows.length)$('entries').insertAdjacentHTML('beforeend',`<div class="empty">${ico('search')}<span>${all.length?'Rien ne passe ces filtres.':'Le client ne contient aucun torrent.'}</span></div>`);
  $('entries').querySelectorAll('.h button').forEach(b=>b.onclick=()=>{st.sort=st.sort.k===b.dataset.k?{k:b.dataset.k,d:-st.sort.d}:{k:b.dataset.k,d:1};renderClient()});
  [...$('entries').querySelectorAll('[data-h]')].forEach(r=>r.onclick=()=>{select(r);const t=all.find(x=>x.hash===r.dataset.h);st.sel=t.path;st.hash=t.hash})}
['clientQ','hideOnSite','onlyDone'].forEach(id=>$(id).oninput=renderClient);
async function fromClient(){st.mode='client';$('modeClient').classList.add('on');$('modeDir').classList.remove('on');$('dirBar').hidden=true;$('clientBar').hidden=false;
  st.sel=null;st.hash=null;$('prepare').disabled=true;$('entries').className='list';listEmpty('hard-drive','Lecture du client…');$('clientNote').hidden=true;
  try{const r=await api('GET','/ui/client/list');st.clientList=r.torrents||[];
    st.clientHint=r.exportable?'le .torrent d\'origine sera exporté et re-scellé, sans re-hachage.':'ce client n\'exporte pas : les données seront re-hachées.';renderClient()}
  catch(e){listEmpty('hard-drive',e.message);$('clientNote').hidden=false;$('clientNote').className='note err';$('clientNote').textContent='Vérifie le client torrent dans les Réglages.'}}

async function cats(){const r=await api('GET','/ui/categories');const all=(r.categories||[]);const slugs=all.map(c=>c.slug);
  // Famille = le plus long autre slug dont celui-ci est l'extension (films-film → films) ; sinon racine.
  const fam=c=>slugs.filter(s=>s!==c.slug&&c.slug.startsWith(s+'-')).sort((a,b)=>b.length-a.length)[0]||c.slug;
  const groups={};all.forEach(c=>{(groups[fam(c)]||=[]).push(c)});
  $('category').innerHTML=Object.keys(groups).sort((a,b)=>a.localeCompare(b)).map(f=>{const root=all.find(c=>c.slug===f);const subs=groups[f].filter(c=>c.slug!==f).sort((a,b)=>a.name.localeCompare(b.name));
    return `<optgroup label="${esc(root?.name||f)}">${(root?[root]:[]).concat(subs).map(c=>`<option value="${esc(c.slug)}">${esc(c.name)}${c.slug===f&&subs.length?' (toute la famille)':''}</option>`).join('')}</optgroup>`}).join('');
  const film=all.find(c=>c.slug==='films-film');if(film)$('category').value=film.slug}

// ---- préparation : hachage → mediainfo → analyse ----
function stage(step,err){const order=['hachage','mediainfo','analyse'];const i=order.indexOf(step);
  [...$('prep').querySelectorAll('.stages span')].forEach((s,k)=>{s.className=err&&k===i?'err':step==='prêt'||k<i?'done':k===i?'on':''});
  $('prog').parentElement.classList.toggle('indet',step!=='hachage'&&step!=='prêt'&&!err)}
$('prepare').onclick=()=>run($('prepare'),async()=>{const r=await api('POST','/ui/prepare',{path:st.sel,category:$('category').value,hash:st.hash||''});st.hash=null;st.job=r.job;
  $('prep').hidden=false;reveal($('prep'),'center');$('prog').style.width='0';stage('hachage');$('prepStep').textContent='';$('facets').innerHTML='';st.picked=null;st.mbPicked=null;st.typesTouched=false;st.captures=[];st.images=[];$('description').value='';$('nfo').value='';$('preview').innerHTML='';$('imgs').innerHTML='';$('imgsNote').hidden=false;st.max=1;$('publish').disabled=false;
  await poll()});
function poll(){return new Promise((res,rej)=>{(async function tick(){try{const j=await api('GET','/ui/job/'+st.job);const[d,t]=j.progress;
    if(j.step==='hachage'){$('prog').style.width=(t?d/t*100:0)+'%';$('prepStep').textContent=t?human(d)+' / '+human(t):(j.exported?'export depuis le client…':'')}else{$('prog').style.width='100%';$('prepStep').textContent=j.step==='mediainfo'?'Lecture des pistes…':j.step==='analyse'?'Analyse par Draupnirr…':''}
    stage(j.step,!!j.error);
    if(!j.done){setTimeout(tick,500);return}
    if(j.error){$('prepStep').innerHTML='<span class="warn-text" style="color:var(--err)">'+esc(j.error)+'</span>';res();return}
    st.miErr=j.mediainfo_error||'';st.analysis=j.analysis;st.torrentName=j.torrent.name;$('prep').hidden=true;
    if(j.category)$('category').value=j.category;musicMode(!!j.analysis.music);go(2);
    if(st.music){musicStart(j.analysis);res();return}
    showAnalysis(j.analysis);
    $('q').value=j.analysis.clean_title||j.torrent.name;$('kind').value=j.analysis.guessed_type==='tv'?'tv':'movie';$('year').value=j.analysis.year||'';$('work_title').value=j.analysis.clean_title||'';$('episode').value='';$('episode_title').value='';
    $('tmdb').innerHTML='';search();res()}catch(e){rej(e)}})()})}

// ---- étape 2 : œuvre et fiche ----
function showAnalysis(a){const n=a.nomenclature;
  $('warnings').innerHTML=(st.miErr?msg('warn','MediaInfo : '+esc(st.miErr)+' — les facettes restent déclarées, Ratatosk les vérifiera.'):'')+(a.warnings||[]).map(w=>msg('warn',esc(w))).join('')+botMsgs(a)||msg('ok','Aucun avertissement : Ratatosk n\'a rien à redire.');
  if(!n){$('built').textContent=a.name;$('chips').innerHTML='';$('legend').hidden=true;$('facets').innerHTML='';$('missing').textContent='';return}
  $('built').textContent=n.built_name||a.name;$('missing').textContent=n.missing?.length?'Manque : '+n.missing.join(', ')+(n.missing_title?' · titre de l\'œuvre':''):(n.missing_title?'Choisis l\'œuvre (titre)':'');
  $('chips').innerHTML=Object.entries(n.facets||{}).map(([k,v])=>`<span class="chip ${v.origin}" title="${esc(k)}">${esc(v.value)}</span>`).join('')+(n.media?Object.values(n.media).filter(Boolean).map(v=>`<span class="chip">${esc(v)}</span>`).join(''):'');$('legend').hidden=!$('chips').children.length;
  if(!$('facets').children.length){const voc=n.vocabulary||{};const decl=['source','edition','group'];
    $('facets').innerHTML=decl.map(f=>f==='group'?`<label>Team<input data-f="group" value="${esc(n.facets?.group?.value||'')}"></label>`:`<label>${f==='source'?'Source':'Édition'}<select data-f="${f}"><option value="">—</option>${(voc[f]||[]).map(t=>`<option ${n.facets?.[f]?.value===t?'selected':''}>${esc(t)}</option>`).join('')}</select></label>`).join('');
    $('facets').querySelectorAll('[data-f]').forEach(el=>el.onchange=reanalyze)}}
function sheet(){const facets={};$('facets').querySelectorAll('[data-f]').forEach(el=>{if(el.value)facets[el.dataset.f]=el.value});
  // Album : le type n'est envoyé qu'une fois touché (absent = types de l'édition MusicBrainz, vide = aucun).
  if(st.music){if(st.typesTouched)facets.type=[...$('types').querySelectorAll('.on')].map(b=>b.dataset.t).join(',');
    return{category:$('category').value,facets,musicbrainz_id:st.mbPicked?.id,release_name:st.analysis?.music?.built_name||undefined}}
  return{category:$('category').value,facets,work_title:$('work_title').value,year:$('year').value,episode:$('episode').value,episode_title:$('episode_title').value,tmdb_id:st.picked?.id,tmdb_type:st.picked?($('kind').value):undefined}}
async function reanalyze(){$('builtBox').classList.add('loading');try{const a=await api('POST','/ui/analyze',{job:st.job,...sheet()});st.analysis=a;st.music&&a.music?showMusic(a):showAnalysis(a)}catch(e){toast('Analyse : '+e.message,'err')}finally{$('builtBox').classList.remove('loading')}}
['work_title','year','episode','episode_title'].forEach(id=>$(id).onchange=reanalyze);

async function search(){$('tmdb').innerHTML='<div class="empty">Recherche sur TMDB…</div>';
  const r=await api('GET','/ui/tmdb?q='+encodeURIComponent($('q').value)+'&type='+$('kind').value).catch(e=>({results:[],error:e.message}));
  $('tmdb').innerHTML=(r.results||[]).slice(0,6).map((x,i)=>`<button type="button" class="c ${st.picked&&st.picked.id===x.id?'sel':''}" data-i="${i}"><img src="${esc(x.poster_url||'')}" alt="" loading="lazy"><div><b>${esc(x.title)}${x.year?' <span class="mut">('+x.year+')</span>':''}</b><div class="mut">${esc(x.overview||'')}</div></div></button>`).join('')||`<div class="empty">${r.error?esc(r.error):'Aucun résultat sur TMDB — tu peux quand même remplir le titre à la main.'}</div>`;
  [...$('tmdb').querySelectorAll('.c')].forEach(d=>d.onclick=()=>{[...$('tmdb').children].forEach(x=>x.classList.remove('sel'));d.classList.add('sel');st.picked=r.results[d.dataset.i];reveal($('facets'));$('work_title').value=st.picked.title;if(st.picked.year)$('year').value=st.picked.year;reanalyze();images()})}
onSubmit($('searchForm'),()=>run($('search'),search));
$('kind').onchange=search;
async function images(){const r=await api('GET','/ui/tmdb-images?id='+st.picked.id+'&type='+$('kind').value+'&episode='+encodeURIComponent($('episode').value)).catch(()=>({images:[]}));st.images=r.images||[];st.captures=[];
  $('imgsNote').hidden=st.images.length>0;if(!st.images.length)$('imgsNote').textContent='Aucune image TMDB pour cette œuvre.';
  $('imgs').innerHTML=st.images.map((im,i)=>`<button type="button" data-i="${i}" title="Ajouter aux captures"><img src="${esc(im.thumb)}" alt="" loading="lazy"></button>`).join('');
  [...$('imgs').children].forEach(im=>im.onclick=()=>{im.classList.toggle('sel');const u=st.images[im.dataset.i].url;st.captures=im.classList.contains('sel')?[...st.captures,u]:st.captures.filter(x=>x!==u);if(im.classList.contains('sel')&&st.step===3)insert(TAGS[$('format').value].img(u)[0]+'\n')})}

// ---- étape 2, version album (docs/25 §6) ----
function musicMode(on){st.music=on;$('videoWork').hidden=on;$('videoFields').hidden=on;$('musicWork').hidden=!on;$('types').hidden=!on;$('tracks').hidden=!on;
  $('p2title').textContent=on?'L\'album et la fiche technique':'L\'œuvre et la fiche technique'}
// Ratatosk parle en block / warn / info.
function botMsgs(a,skip=[]){return(a.bot||[]).filter(b=>!skip.includes(b.code)).map(b=>msg(b.level==='block'?'err':b.level==='warn'?'warn':'info','Ratatosk : '+esc(b.message))).join('')}
function musicStart(a){const m=a.music;$('types').innerHTML='';$('tracks').innerHTML='';$('mb').innerHTML='';
  // Tags Picard : l'édition est déjà connue, la grille la montre sélectionnée.
  if(m.musicbrainz_id)st.mbPicked={id:m.musicbrainz_id,tags:true};
  showMusic(a);$('mbArtist').value=m.artist||'';$('mbAlbum').value=m.album||'';
  if(m.album)mbSearch();else $('mb').innerHTML='<div class="empty">Pas de tag « album » dans les pistes : tape l\'artiste et l\'album pour chercher l\'édition.</div>'}
function showMusic(a){const m=a.music,voc=m.vocabulary||{};
  $('warnings').innerHTML=(st.miErr?msg('warn','MediaInfo : '+esc(st.miErr)):'')+(m.probe_error?msg('err',esc(m.probe_error)):'')
    +(!m.sheet&&!m.probe_error?msg('warn','Pistes non lues : sans rapport MediaInfo, la release garde le nom du dossier et un NFO est à joindre.'):'')
    +(m.rip_log?msg('info','Un .log ou un .cue accompagne les pistes : source CD déduite.'):'')
    +(st.mbPicked?.tags&&st.mbPicked.id===m.musicbrainz_id?msg('info','Édition MusicBrainz lue dans les tags des pistes.'):'')
    +(m.issues||[]).map(i=>msg('warn',esc(i.message))).join('')+(a.warnings||[]).map(w=>msg('warn',esc(w))).join('')
    // Les espaces du dossier ne comptent pas : c'est le nom calculé qui sera publié.
    +botMsgs(a,m.built_name?['naming_spaces']:[])||msg('ok','Aucun avertissement : Ratatosk n\'a rien à redire.');
  $('built').textContent=m.built_name||st.torrentName||a.name;$('legend').hidden=true;
  $('missing').textContent=m.missing?.length?'Manque : '+m.missing.join(', ')+' — sans quoi la release garde le nom de son dossier.':'';
  $('chips').innerHTML=[m.format_label,m.source,...(m.types||[]),m.discs>1?m.discs+' disques':'',m.track_count?m.track_count+' pistes':'',m.duration,m.label,m.year].filter(Boolean).map(v=>`<span class="chip">${esc(v)}</span>`).join('');
  $('tracks').innerHTML=(m.tracks||[]).map((t,i)=>`<li><span class="mut">${m.discs>1?t.disc+'-':''}${String(t.number??i+1).padStart(2,'0')}</span><span>${esc(t.title)}</span><span class="mut d">${esc(t.duration)}</span></li>`).join('');
  if(!$('facets').children.length){
    $('facets').innerHTML=`<label>Source<select data-f="source"><option value="">—</option>${(voc.source||[]).map(s=>`<option ${m.source===s?'selected':''}>${esc(s)}</option>`).join('')}</select></label><label>Team<input data-f="group" value="${esc(m.group||'')}"></label>`;
    $('facets').querySelectorAll('[data-f]').forEach(el=>el.onchange=reanalyze)}
  $('types').innerHTML='<span class="lbl" title="Pré-rempli depuis l\'édition MusicBrainz">Type</span>'+(voc.type||[]).map(t=>{const on=(m.types||[]).includes(t);return`<button type="button" data-t="${esc(t)}" class="${on?'on':''}" aria-pressed="${on}">${esc(t)}</button>`}).join('')}
$('types').addEventListener('click',e=>{const b=e.target.closest('[data-t]');if(!b)return;b.setAttribute('aria-pressed',b.classList.toggle('on'));st.typesTouched=true;reanalyze()});
async function mbSearch(){const m=st.analysis?.music||{};$('mb').innerHTML='<div class="empty">Recherche sur MusicBrainz…</div>';
  const q=new URLSearchParams({artist:$('mbArtist').value,album:$('mbAlbum').value});if(m.track_count)q.set('tracks',m.track_count);
  const r=await api('GET','/ui/musicbrainz?'+q).catch(e=>({results:[],error:e.message}));st.mbResults=r.results||[];
  $('mb').innerHTML=st.mbResults.map((x,i)=>`<button type="button" class="c ${st.mbPicked?.id===x.id?'sel':''}" data-i="${i}"><img src="${esc(x.cover_url||'')}" alt="" loading="lazy" onerror="this.style.visibility='hidden'"><div><b>${esc(x.title)}${x.year?' <span class="mut">('+x.year+')</span>':''}</b><div class="mut">${esc([x.artist,x.label,x.country,x.media,x.disambiguation].filter(Boolean).join(' · '))}</div>${x.tracks_match?`<span class="bdg seed">${x.track_count} pistes, comme ton dossier</span>`:`<span class="bdg x">${x.track_count} pistes</span>`}</div></button>`).join('')
    ||`<div class="empty">${r.error?esc(r.error):'Aucune édition sur MusicBrainz — le nom viendra des tags des pistes.'}</div>`;
  [...$('mb').querySelectorAll('.c')].forEach(d=>d.onclick=()=>{const x=st.mbResults[d.dataset.i];const off=st.mbPicked?.id===x.id&&!st.mbPicked.tags;
    [...$('mb').children].forEach(c=>c.classList.remove('sel'));if(off)st.mbPicked=null;else{d.classList.add('sel');st.mbPicked=x}reanalyze()})}
onSubmit($('mbForm'),()=>run($('mbSearch'),mbSearch));

// ---- étape 3 : présentation ----
$('toPres').onclick=()=>run($('toPres'),async()=>{if(!st.templates.length){const c=await api('GET','/ui/presentations').catch(()=>({templates:[]}));st.templates=c.templates||[]}
  const def=presDefault($('category').value.split('-')[0]);
  $('tpl').innerHTML='<option value="">— aucun (description libre) —</option>'+st.templates.map((t,i)=>`<option value="${i}" ${t===def?'selected':''}>${esc(t.name)}${t.site?' · Draupnirr':''}${t.family?' · '+esc(t.family):''}</option>`).join('');
  $('capsBox').hidden=st.music;go(3);if(!$('description').value)regen();fillVars();schedulePreview()});
// Le modèle par défaut, comme presDefault() du site (docs/25 §4.2) : celui du membre pour la famille, puis
// toutes catégories ; s'il n'a AUCUN modèle pour la famille, celui du site. Des modèles sans défaut = à la main.
function presDefault(fam){const c=st.templates.filter(t=>!t.family||t.family===fam),own=c.filter(t=>!t.site);
  if(own.length)return own.find(t=>t.is_default&&t.family===fam)||own.find(t=>t.is_default&&!t.family)||null;
  const site=c.filter(t=>t.site&&t.family===fam);return site.find(t=>t.is_default)||site[0]||null}
// {{pistes}} : liste numérotée selon le format du modèle.
function pistes(tracks,fmt){const it=(tracks||[]).map(t=>(fmt==='html'?esc(t.title):t.title)+(t.duration?' ('+t.duration+')':''));
  return!it.length?'':fmt==='html'?'<ol><li>'+it.join('</li><li>')+'</li></ol>':'[list=1]\n[*]'+it.join('\n[*]')+'\n[/list]'}
function vars(){const v=videoVars(),a=st.analysis||{},al=a.music;if(!al)return v;
  // Album (docs/25 §6.7) : titre, affiche et durée aussi, pour qu'un modèle film reste lisible.
  return{...v,titre:al.album||'',annee:String(al.year||''),type:'Album',affiche:al.cover_url||'',nom_release:al.built_name||a.name||'',
    source:al.source||'',team:al.group||'',duree:al.duration||'',nfo:al.nfo||'',tags:(al.tags||[]).join(', '),episode:'',titre_episode:'',
    artiste:al.artist||'',album:al.album||'',label:al.label||'',format_audio:al.format_label||'',pistes:pistes(al.tracks,$('format').value),
    nb_pistes:String(al.track_count||''),musicbrainz_url:al.musicbrainz_url||''}}
function videoVars(){const a=st.analysis||{},n=a.nomenclature||{},f=n.facets||{},v=k=>f[k]?.value||'',m=n.media||{},fmt=$('format').value,p=st.picked||{};
  const caps=st.captures.map(u=>fmt==='html'?`<img src="${u}" alt="">`:`[img]${u}[/img]`).join('\n');
  return{titre:p.title||a.clean_title||'',annee:$('year').value,type:$('kind').value==='tv'?'Série':'Film',synopsis:p.overview||'',affiche:p.poster_url||'',tmdb_url:p.id?`https://www.themoviedb.org/${$('kind').value}/${p.id}`:'',nom_release:n.built_name||a.name||'',taille:a.size_human||'',nb_fichiers:String(a.file_count||''),fichiers:(a.files||[]).map(x=>`${x.path} (${x.size_human})`).join('\n'),episode:$('episode').value,titre_episode:$('episode_title').value,tags:(a.suggested_tags||[]).join(', '),source:v('source'),edition:v('edition'),team:v('group'),langues:v('languages'),resolution:v('resolution'),codec_video:v('video_codec'),profondeur:v('bit_depth'),hdr:v('hdr'),codec_audio:v('audio_codec'),canaux:v('channels'),duree:m.duration||'',debit:m.bitrate||'',sous_titres:m.subtitles||'',mediainfo:'',nfo:n.nfo||'',captures:caps,uploadeur:$('anonymous').checked?'Anonyme':(st.me?.name||''),date:new Date().toLocaleDateString('fr-FR')}}
function render(body,d){let out=body;for(;;){const s=out.indexOf('{{#');if(s<0)break;const e=out.indexOf('}}',s);const name=out.slice(s+3,e);const close='{{/'+name+'}}';const c=out.indexOf(close,s);if(c<0)break;out=out.slice(0,s)+(d[name]?out.slice(e+2,c):'')+out.slice(c+close.length)}
  return out.replace(/\{\{\s*([a-z_]+)\s*\}\}/g,(m,k)=>d[k]??'').replace(/[ \t]+$/gm,'').replace(/\n{3,}/g,'\n\n').trim()}
function regen(){const t=st.templates[$('tpl').value];if(!t){return}$('format').value=t.format;$('description').value=render(t.body,vars());schedulePreview()}
$('regen').onclick=regen;$('tpl').onchange=regen;

// ---- éditeur assisté : balises autour de la sélection, selon le format ----
const TAGS={
  bbcode:{b:['[b]','[/b]'],i:['[i]','[/i]'],u:['[u]','[/u]'],h2:['[h2]','[/h2]'],size:v=>[`[size=${v}]`,'[/size]'],color:v=>[`[color=${v}]`,'[/color]'],center:['[center]','[/center]'],quote:['[quote]','[/quote]'],list:['[list]\n[*]','\n[/list]'],item:['[*]',''],link:u=>[`[url=${u}]`,'[/url]'],img:u=>[`[img]${u||'URL'}[/img]`,''],code:['[code]','[/code]'],spoiler:['[spoiler]','[/spoiler]'],hr:['[hr]\n','']},
  html:{b:['<b>','</b>'],i:['<i>','</i>'],u:['<u>','</u>'],h2:['<h2>','</h2>'],size:v=>[`<span style="font-size:${v}%">`,'</span>'],color:v=>[`<span style="color:${v}">`,'</span>'],center:['<center>','</center>'],quote:['<blockquote>','</blockquote>'],list:['<ul>\n<li>','</li>\n</ul>'],item:['<li>','</li>'],link:u=>[`<a href="${u}">`,'</a>'],img:u=>[`<img src="${u||'URL'}">`,''],code:['<code>','</code>'],spoiler:['<details><summary>Spoiler</summary>','</details>'],hr:['<hr>\n','']}};
const ta=$('description');
// wrap : entoure la sélection ; sans sélection, le curseur reste dans la balise ouverte.
function wrap([before,after],sel){const t=ta;const s=sel?sel[0]:t.selectionStart,e=sel?sel[1]:t.selectionEnd;const mid=t.value.slice(s,e);
  t.value=t.value.slice(0,s)+before+mid+after+t.value.slice(e);t.focus();
  if(mid)t.setSelectionRange(s+before.length,s+before.length+mid.length);else t.setSelectionRange(s+before.length,s+before.length);schedulePreview()}
function insert(text){wrap([text,''])}
function cmd(k,arg){const d=TAGS[$('format').value][k];const sel=st.edSel;st.edSel=null;wrap(typeof d==='function'?d(arg):d,sel)}
$('tb').addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;
  if(b.dataset.cmd){cmd(b.dataset.cmd);return}
  st.edSel=[ta.selectionStart,ta.selectionEnd];pop(b.dataset.pop)});
ta.addEventListener('keydown',e=>{if(!(e.metaKey||e.ctrlKey))return;const k={b:'b',i:'i',u:'u'}[e.key.toLowerCase()];if(k){e.preventDefault();cmd(k)}});
const SWATCHES=['#E04848','#E8A33D','#3FAE5C','#3E8ED0','#9B6BD6','#8A94A6'];
function pop(kind){const p=$('pop');if(p.dataset.kind===kind&&!p.hidden){closePop();return}p.dataset.kind=kind;p.hidden=false;
  if(kind==='color')p.innerHTML=SWATCHES.map(c=>`<button type="button" class="sw" data-v="${c}" style="background:${c}" title="${c}"></button>`).join('');
  else if(kind==='size')p.innerHTML=[['90','Petit'],['130','Grand'],['200','Très grand']].map(([v,l])=>`<button type="button" data-v="${v}">${l}</button>`).join('');
  else{p.innerHTML=`<input type="url" placeholder="${kind==='img'?'https://…/image.jpg':'https://…'}" autocomplete="off"><button type="button" class="primary" data-ok="1">Insérer</button>`;p.querySelector('input').focus()}}
function closePop(){$('pop').hidden=true;$('pop').dataset.kind=''}
$('pop').addEventListener('click',e=>{const b=e.target.closest('button');if(!b)return;const k=$('pop').dataset.kind;
  if(b.dataset.v){cmd(k,b.dataset.v);closePop();return}
  const u=$('pop').querySelector('input').value.trim();if(!u)return;cmd(k,u);closePop()});
$('pop').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();$('pop').querySelector('[data-ok]')?.click()}if(e.key==='Escape')closePop()});
document.addEventListener('click',e=>{if(!$('pop').hidden&&!e.target.closest('#pop')&&!e.target.closest('[data-pop]'))closePop()});
// Palette de variables : la VALEUR rendue est insérée, le modèle reste sur le site.
function fillVars(){const v=vars();$('varSel').innerHTML='<option value="">Insérer une variable…</option>'+Object.keys(v).map(k=>`<option value="${k}">${k} — ${esc(String(v[k]).replace(/\s+/g,' ').slice(0,40))||'(vide)'}</option>`).join('')}
$('varSel').onchange=()=>{const k=$('varSel').value;if(k)insert(String(vars()[k]||''));$('varSel').value=''};
['year','episode','episode_title','anonymous','format'].forEach(id=>$(id).addEventListener('change',fillVars));

// ---- aperçu en temps réel : debounce 500 ms, réponse périmée ignorée ----
let pvTimer=0,pvSeq=0;
function schedulePreview(){clearTimeout(pvTimer);$('pvState').textContent='rendu…';pvTimer=setTimeout(renderPreview,500)}
async function renderPreview(){const seq=++pvSeq;const content=ta.value;if(!content.trim()){$('preview').innerHTML='';$('pvState').textContent='';return}
  try{const r=await api('POST','/ui/preview',{content,format:$('format').value});if(seq!==pvSeq)return;$('preview').innerHTML=r.html;$('pvState').textContent=''}
  catch(e){if(seq!==pvSeq)return;$('pvState').textContent='aperçu indisponible : '+e.message}}
ta.addEventListener('input',schedulePreview);
$('format').addEventListener('change',schedulePreview);
$('editor').querySelectorAll('.tabs button').forEach(b=>b.onclick=()=>{$('editor').dataset.tab=b.dataset.tab;$('editor').querySelectorAll('.tabs button').forEach(x=>x.classList.toggle('on',x===b))});

// ---- étape 4 : publication ----
$('publish').onclick=()=>run($('publish'),async()=>{
  const al=st.analysis?.music;
  // Album : le serveur reprend nom, NFO, pochette, titre et tags de la fiche et de l'édition.
  const meta=al?{musicbrainz_id:st.mbPicked?.id||al.musicbrainz_id||undefined,facets:sheet().facets}:{...sheet(),poster_url:st.picked?.poster_url,synopsis:st.picked?.overview,tags:(st.analysis?.suggested_tags||[])};
  const r=await api('POST','/ui/publish',{job:st.job,category:$('category').value,description:$('description').value,description_format:$('format').value,nfo_text:$('nfo').value,meta});
  $('publish').disabled=true;$('publish').classList.remove('busy');
  $('pubTitle').textContent=r.result.awaiting_validation?'Publié, en attente de Ratatosk':'Publié au catalogue';
  $('pub').innerHTML=msg('ok',r.result.awaiting_validation?'<b>En attente de validation Ratatosk</b> — garde le seed, la release passe au catalogue dès qu\'elle est vérifiée.':'<b>Au catalogue</b> — la release est visible par les membres.')
    +(r.client_added?msg('ok',`Ajouté à ${esc(r.client_added)} sur les mêmes données${$('cl_skip').checked?', sans re-vérification':''}.`):'')
    +(r.client_error?msg('warn','Client torrent : '+esc(r.client_error)):'')
    +(r.saved?msg('ok','.torrent personnalisé enregistré : <span class="mono">'+esc(r.saved)+'</span>'+(r.client_added?'':' — ajoute-le à ton client sur les mêmes données.')):msg('warn',esc(r.download_error||'Le .torrent personnalisé n\'a pas pu être récupéré.')));
  $('pubLink').href=r.url;go(4);});
$('again').onclick=()=>{st.job=null;st.analysis=null;st.picked=null;st.mbPicked=null;st.typesTouched=false;musicMode(false);$('mb').innerHTML='';st.captures=[];st.images=[];st.sel=null;st.hash=null;st.max=1;
  $('facets').innerHTML='';$('description').value='';$('nfo').value='';$('preview').innerHTML='';$('imgs').innerHTML='';$('tmdb').innerHTML='';$('pub').innerHTML='';$('publish').disabled=false;$('prepare').disabled=true;
  go(1);if(st.mode==='client')fromClient();else browse($('path').value).catch(e=>toast(e.message,'err'))};

load().catch(e=>{$('connect').hidden=false;$('connectErr').textContent=e.message});


// ---- Vue Cross-seed ----
function view(name){const cross=name==='cross',batch=name==='batch',up=!cross&&!batch;$('viewCross').classList.toggle('on',cross);$('viewBatch').classList.toggle('on',batch);$('viewUpload').classList.toggle('on',up);
  document.querySelector('.bridge').hidden=!up;[1,2,3,4].forEach(k=>$('p'+k).hidden=!up||k!==st.step);$('pX').hidden=!cross;$('pB').hidden=!batch;if(batch)batchInit();scrollTop()}
$('viewUpload').onclick=()=>view('upload');$('viewCross').onclick=()=>view('cross');$('viewBatch').onclick=()=>view('batch');
st.cross=[];
function crossRow(r){const m=r.match;const key=esc(r.hash);
  return `<div class="x ${m?'':'none'}" data-h="${key}"><span class="nm" title="${esc(r.path)}"><b>${esc(r.name)}</b><small>${esc(r.path)}</small></span><span class="r">${human(r.size)}</span>`+
    (m?`<span class="nm"><b>${esc(m.name)}</b><small>${esc(m.category||'')} · ${m.seeders??0} seeder(s) · ${Math.round((r.score||0)*100)} % de ressemblance</small></span><span><button type="button" class="primary small" data-add="${key}"><svg class="i"><use href="#i-link"/></svg>Cross-seeder</button></span>`
      :`<span class="mut">aucune release de cette taille sur Draupnirr</span><span></span>`)+`</div>`}
function renderCross(){const only=$('crossOnly').checked;const rows=st.cross.filter(r=>!only||r.match);const n=st.cross.filter(r=>r.match).length;
  $('crossCount').textContent=st.cross.length?`${n} correspondance(s) sur ${st.cross.length} torrent(s) hors Draupnirr`:'';
  $('crossAll').hidden=n===0;$('crossAll').innerHTML=`${ico('link')}Tout cross-seeder (${n})`;$('crossOnlyWrap').hidden=st.cross.length===0;
  $('crossList').innerHTML='<div class="x h"><span>Dans ton client</span><span class="r">Taille</span><span>Sur Draupnirr</span><span></span></div>'+(rows.map(crossRow).join('')||'<div class="empty"><span>Rien à afficher.</span></div>');
  [...$('crossList').querySelectorAll('[data-add]')].forEach(b=>b.onclick=()=>crossAdd(b.dataset.add,b))}
async function crossAdd(hash,btn){const r=st.cross.find(x=>x.hash===hash);if(!r||!r.match)return;const row=btn.closest('.x');const cell=btn.parentElement;
  await run(btn,async()=>{try{const res=await api('POST','/ui/crossseed/add',{path:r.path,id:r.match.id});r.done=true;cell.innerHTML=`<span class="ok">${ico('check')} ajouté au client</span>`;toast('Cross-seed ajouté : '+res.name,'ok')}
    catch(e){const msg=(e.problems||[]).join(' · ')||e.message;cell.innerHTML=`<span class="err" title="${esc(msg)}">${ico('alert')} refusé</span>`;row.title=msg;toast(msg,'err')}})}
$('crossScan').onclick=()=>run($('crossScan'),async()=>{$('crossList').innerHTML='<div class="empty"><span>Lecture du client et comparaison avec Draupnirr…</span></div>';
  try{const r=await api('GET','/ui/crossseed/scan');st.cross=(r.rows||[]).map(x=>({...x,done:false}));renderCross();toast(`${r.matched} correspondance(s) trouvée(s)`,r.matched?'ok':'info')}
  catch(e){st.cross=[];renderCross();$('crossList').innerHTML=`<div class="empty"><span>${esc(e.message)}</span></div>`;toast(e.message,'err')}});
$('crossOnly').onchange=renderCross;
$('crossAll').onclick=()=>run($('crossAll'),async()=>{const todo=st.cross.filter(r=>r.match&&!r.done);let ok=0,ko=0;
  for(const r of todo){const btn=$('crossList').querySelector(`[data-add="${CSS.escape(r.hash)}"]`);if(!btn)continue;await crossAdd(r.hash,btn);r.done?ok++:ko++}
  toast(`Cross-seed terminé : ${ok} ajouté(s), ${ko} refusé(s)`,ko?'warn':'ok')});


// ---- Vue Lot ----
st.batchTimer=null;
function batchInit(){if(!$('batchPath').value)$('batchPath').value=$('path').value||'';
  const cats=[...$('category').options].map(o=>`<option value="${esc(o.value)}">${esc(o.textContent)}</option>`).join('');
  if(!$('batchCatFilm').options.length){$('batchCatFilm').innerHTML=cats;$('batchCatTV').innerHTML=cats;$('batchCatFilm').value='films-film';$('batchCatTV').value='series-serie-tv'}
  batchMode();batchPoll();if($('batchPath').value&&!st.batchPick)batchPickLoad().catch(()=>{})}
// Une seule liste : les entrées du dossier, cochables tant qu'aucun lot ne tourne, avec l'état
// que le lot leur donne. Tri par clic sur un en-tête, second clic pour inverser ; sans clic, l'ordre du dossier.
st.batchSort=null;st.batchJob=null;
// États d'une ligne du lot : les libellés que le serveur envoie (rowStatus dans batch.go), plus « exclu », propre à la page.
const BS=Object.freeze({wait:'attente',run:'en cours',pub:'publié',sim:'simulé',rev:'à revoir',err:'erreur',present:'déjà présent',ign:'ignoré',off:'exclu'});
const bRank=s=>[BS.run,BS.wait,BS.pub,BS.sim,BS.rev,BS.err,BS.present,BS.ign,'',BS.off].indexOf(s||'');
const bBadge=s=>({[BS.wait]:'wait',[BS.run]:'run',[BS.pub]:'pub',[BS.sim]:'pub',[BS.present]:'wait',[BS.ign]:'wait',[BS.rev]:'rev',[BS.err]:'err'})[s]||'wait';
function renderBatch(j){st.batchJob=j;const rows=j.rows||[];
  if(rows.length||j.running){$('batchSummary').hidden=false;
    const todo=rows.filter(r=>r.status!==BS.present),done=todo.filter(r=>r.status!==BS.wait&&r.status!==BS.run).length,cur=rows.find(r=>r.status===BS.run);
    const total=j.limit>0?Math.min(j.limit,todo.length):todo.length;const pct=total?Math.round(done/total*100):0;
    $('batchProg').style.width=pct+'%';$('batchPct').textContent=`${done} / ${total} · ${pct} %`;
    $('batchNow').textContent=cur?`En cours : ${cur.name} — ${cur.detail||''}`:(j.running?'Préparation…'+(j.note||''):(j.done?'Terminé.':''));
    $('batchCount').textContent=`${rows.length} élément(s) · ${j.skipped||0} déjà présent(s) · ${j.published||0} ${j.dry_run?'publiable(s)':'publié(s)'} · ${j.review||0} à revoir`+(j.running?' · en cours…':j.done?' · terminé':'')+(j.error?' · '+j.error:'')}
  $('batchStop').hidden=!j.running;$('batchStart').disabled=!!j.running;
  renderBatchTable()}
// Dernière décision connue (historique), datée, tant que le lot ne l'a pas remplacée. Décochée, l'entrée reste « exclu » et le détail dit pourquoi.
function lastSeen(l,on){if(!l)return {};const d=new Date(l.at).toLocaleDateString('fr-FR',{day:'2-digit',month:'2-digit'});
  return {status:on?l.status:BS.off,detail:`${on?'':l.status+' · '}${d}${l.detail?' · '+l.detail:''}`,url:l.url||'',built:l.built,tmdb:l.tmdb,edition:l.edition}}
// Déjà sur Draupnirr d'après l'historique : décochée par défaut (le lot la sauterait de toute façon).
const bDone=l=>!!l&&(l.status===BS.pub||l.status===BS.present);
function renderBatchTable(){const j=st.batchJob||{},jobRows=j.rows||[],running=!!j.running,pk=st.batchPick,
    // Un lot qui tourne sur un autre dossier garde son tableau ; la liste à cocher revient quand il s'arrête.
    p=pk&&pk.key===batchPickKey()&&!(running&&j.root!==pk.path)?pk:null;
  const byPath=new Map(jobRows.map(r=>[r.path,r]));
  // Pendant un lot, l'état vient du serveur ; sinon une entrée décochée est « exclu », même si un lot précédent l'a vue.
  const rows=p?p.entries.map(e=>{const on=p.on.has(e.path),r=byPath.get(e.path);
      return {...(r&&(running||on)?r:{name:e.name,size:e.size,path:e.path,status:on?'':BS.off,...lastSeen(e.last,on)}),is_dir:e.is_dir,on,pick:true}}):jobRows;
  $('batchPickAll').hidden=$('batchPickNone').hidden=!p||!p.entries.length||running;
  if(p){const n=p.entries.length,k=p.on.size;
    $('batchPickCount').textContent=n?`${k} / ${n} ${$('batchMusic').checked?'album(s)':'élément(s)'} inclus dans le lot`:'Rien à traiter dans ce dossier.'}
  if(!rows.length){$('batchList').innerHTML=`<div class="empty">${ico('folder')}<span>${p?'Ce dossier est vide.':'Choisis un dossier : ses entrées s\'affichent ici, à cocher.'}</span></div>`;return}
  const so=st.batchSort,sorted=so?[...rows].sort((a,b)=>so.d*(so.k==='size'?a.size-b.size:so.k==='status'?bRank(a.status)-bRank(b.status)||a.name.localeCompare(b.name):a.name.localeCompare(b.name))):rows;
  const h=(...a)=>sortHead(so,...a);
  $('batchList').innerHTML=`<div class="b h"><span></span>${h('name','Release')}${h('size','Taille','justify-content:flex-end')}${h('status','État')}<span class="eyebrow" style="line-height:34px">Détail</span></div>`+sorted.map(r=>`<div class="b ${r.pick&&!r.on?'off':''} ${r.pick&&!running?'pk':''}"><span>${r.pick?`<input type="checkbox" data-p="${esc(r.path)}" ${r.on?'checked':''} ${running?'disabled':''}>`:''}</span><span class="nm" title="${esc(r.path)}"><b>${esc(r.name)}</b><small>${esc([r.built||r.tmdb,r.edition&&'MusicBrainz : '+r.edition].filter(Boolean).join(' · '))}</small></span><span class="r">${human(r.size)}</span><span>${r.status?`<span class="bdg ${bBadge(r.status)}">${esc(r.status)}</span>`:'<span class="mut dash">—</span>'}</span><span class="mut" style="font-size:12px">${r.url?`<a href="${esc(r.url)}" target="_blank">${esc(r.detail)}</a>`:esc(r.detail||'')}</span></div>`).join('');
  $('batchList').querySelectorAll('input[type=checkbox]').forEach(c=>c.onchange=()=>{c.checked?p.on.add(c.dataset.p):p.on.delete(c.dataset.p);renderBatchTable()});
  // Toute la ligne coche ou décoche, sauf un clic sur la case elle-même ou sur un lien.
  $('batchList').querySelectorAll('.b.pk').forEach(row=>row.onclick=e=>{if(e.target.closest('input,a'))return;const c=row.querySelector('input');c.checked=!c.checked;c.onchange()});
  $('batchList').querySelectorAll('.h button').forEach(b=>b.onclick=()=>{st.batchSort=st.batchSort&&st.batchSort.k===b.dataset.k?{k:b.dataset.k,d:-st.batchSort.d}:{k:b.dataset.k,d:1};renderBatchTable()})}
async function batchPoll(){clearTimeout(st.batchTimer);try{const j=await api('GET','/ui/batch/status');renderBatch(j);if(j.running)st.batchTimer=setTimeout(batchPoll,2000)}catch(e){}}
$('batchStart').onclick=()=>run($('batchStart'),async()=>{const dry=$('batchDry').checked;
  if(st.batchPick&&st.batchPick.key===batchPickKey()&&st.batchPick.entries.length&&!st.batchPick.on.size)return toast('Tout est décoché : rien à traiter.','warn');
  if(!dry&&!confirm('Publier pour de vrai ce qui est sûr ? Les releases douteuses resteront « à revoir ».'))return;
  await api('POST','/ui/batch/start',{path:$('batchPath').value.trim(),dry_run:dry,max:+$('batchMax').value||0,limit:+$('batchLimit').value||0,max_size:Math.round((+$('batchMaxSize').value||0)*1024**3),only_video:$('batchVideo').checked,music:$('batchMusic').checked,music_source:$('batchSrc').value,category_film:$('batchCatFilm').value,category_tv:$('batchCatTV').value,include:batchIncluded()});
  toast(dry?'Simulation lancée':'Lot lancé','ok');batchPoll()});
// Musique et « vidéos seulement » s'excluent ; les catégories film/série ne servent pas aux albums.
function batchMode(){const m=$('batchMusic').checked;$('batchSrcWrap').hidden=!m;$('batchCatFilm').parentElement.hidden=m;$('batchCatTV').parentElement.hidden=m}
$('batchMusic').onchange=()=>{if($('batchMusic').checked)$('batchVideo').checked=false;batchMode();if(st.batchPick)batchPickLoad().catch(e=>toast(e.message,'err'))};
$('batchVideo').onchange=()=>{if($('batchVideo').checked)$('batchMusic').checked=false;batchMode()};
// Cases à cocher : une par entrée du dossier (par album en musique), toutes cochées au départ, sauf celles déjà sur Draupnirr d'après l'historique.
// Seules les cochées partent au serveur ; une liste d'un autre dossier ou d'un autre mode ne compte pas.
st.batchPick=null;
const batchPickKey=()=>$('batchPath').value.trim()+'|'+($('batchMusic').checked?1:0);
// null = pas de liste pour ce dossier : tout y passe. Sinon les seules cochées, pour ne publier que ce qui a été montré.
function batchIncluded(){const p=st.batchPick;if(!p||p.key!==batchPickKey())return null;return p.entries.filter(e=>p.on.has(e.path)).map(e=>e.path)}
async function batchPickLoad(){const path=$('batchPath').value.trim();if(!path)return toast('Choisis un dossier','warn');const key=batchPickKey();
  $('batchPickCount').textContent='Lecture du dossier…';
  const r=await api('GET','/ui/batch/list?path='+encodeURIComponent(path)+($('batchMusic').checked?'&music=1':''));if(key!==batchPickKey())return;
  const prev=st.batchPick&&st.batchPick.key===key?st.batchPick:null;const entries=r.entries||[];
  st.batchPick={key,path,entries,on:new Set(entries.filter(e=>prev&&prev.entries.some(x=>x.path===e.path)?prev.on.has(e.path):!bDone(e.last)).map(e=>e.path))};renderBatchTable()}
$('batchPickLoad').onclick=()=>run($('batchPickLoad'),()=>batchPickLoad().catch(e=>{$('batchPickCount').textContent=e.message;toast(e.message,'err')}));
$('batchPath').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();$('batchPickLoad').click()}});
$('batchPath').addEventListener('change',()=>{if(st.batchPick&&st.batchPick.key!==batchPickKey())$('batchPickLoad').click()});
$('batchPickAll').onclick=()=>{st.batchPick.entries.forEach(e=>st.batchPick.on.add(e.path));renderBatchTable()};
$('batchPickNone').onclick=()=>{st.batchPick.on.clear();renderBatchTable()};
$('batchStop').onclick=()=>api('POST','/ui/batch/stop').then(()=>toast('Arrêt demandé après la release en cours','info'));
