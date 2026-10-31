(() => {
  'use strict';

  // Connects the account's own Hyperliquid wallet: the browser wallet picks
  // the account, signs the approvals the engine prepared, and the engine
  // checks them with the exchange. No key leaves the browser wallet.
  const box = document.getElementById('wallet');
  const button = box && box.querySelector('[data-connect]');
  if (!button) return;
  const status = box.querySelector('[data-status]');
  const say = (text) => { status.textContent = text; };

  const post = async (path, body) => {
    const resp = await fetch(path, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': box.dataset.csrf },
      body: JSON.stringify(body),
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || 'That did not work; try again in a minute.');
    return data;
  };

  // A signature as eth_signTypedData_v4 returns it, split as the exchange
  // takes it.
  const split = (sig) => {
    let v = parseInt(sig.slice(130, 132), 16);
    if (v < 27) v += 27;
    return { r: '0x' + sig.slice(2, 66), s: '0x' + sig.slice(66, 130), v };
  };

  button.addEventListener('click', async () => {
    const eth = window.ethereum;
    if (!eth) {
      say('No browser wallet found. Install MetaMask or Rabby, then reload this page.');
      return;
    }
    button.disabled = true;
    try {
      say('Choose the account in your wallet…');
      const [user] = await eth.request({ method: 'eth_requestAccounts' });
      const chainId = parseInt(await eth.request({ method: 'eth_chainId' }), 16);
      const approvals = await post('/wallet/approvals', { user, chain_id: chainId });
      const sign = async (approval, what) => {
        say('Sign ' + what + ' in your wallet. It costs nothing and moves no money.');
        const sig = await eth.request({ method: 'eth_signTypedData_v4', params: [user, JSON.stringify(approval.typed_data)] });
        return { action: approval.action, nonce: approval.nonce, signature: split(sig) };
      };
      const body = { user, approve_agent: await sign(approvals.approve_agent, 'the agent wallet') };
      if (approvals.approve_builder_fee) body.approve_builder_fee = await sign(approvals.approve_builder_fee, 'the fee per trade');
      say('Checking with Hyperliquid…');
      await post('/wallet/approve', body);
      location.reload();
    } catch (err) {
      say(err && err.code === 4001 ? 'You declined in your wallet; nothing changed.' : (err && err.message) || String(err));
      button.disabled = false;
    }
  });
})();
