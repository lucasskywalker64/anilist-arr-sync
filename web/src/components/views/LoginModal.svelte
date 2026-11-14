<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import Button from '../common/Button.svelte';
  import Alert from '../common/Alert.svelte';
  import { api, getBaseUrl } from '../../lib/api';

  export let isSetup = false;

  const dispatch = createEventDispatcher<{ success: { username: string } }>();

  let username = '';
  let password = '';
  let confirmPassword = '';
  let loading = false;
  let errorMsg = '';

  async function handleSubmit() {
    errorMsg = '';
    if (!username.trim() || !password) {
      errorMsg = 'Username and password are required.';
      return;
    }

    if (isSetup && password !== confirmPassword) {
      errorMsg = 'Passwords do not match.';
      return;
    }

    loading = true;
    try {
      if (isSetup) {
        const res = await api.setup({ username: username.trim(), password });
        dispatch('success', { username: res.username });
      } else {
        const res = await api.login({ username: username.trim(), password });
        dispatch('success', { username: res.username });
      }
    } catch (e: any) {
      if (e.message && e.message.includes('setup already completed')) {
        isSetup = false;
        errorMsg = 'Setup was already completed. Please log in.';
      } else {
        errorMsg = e.message || (isSetup ? 'Setup failed' : 'Invalid username or password');
      }
    } finally {
      loading = false;
    }
  }
</script>

<div class="loginOverlay">
  <div class="loginCard">
    <div class="cardHeader">
      <img class="logo" src="./favicon.svg" alt="AniList-Arr-Sync" width="48" height="48" />
      <h2 class="title">{isSetup ? 'Initial Administrator Setup' : 'AniList-Arr-Sync Login'}</h2>
      <p class="subtitle">
        {isSetup ? 'Configure an initial administrator account to protect your synchronization instance' : 'Sign in to access your instance'}
      </p>
    </div>

    {#if errorMsg}
      <Alert kind="danger">{errorMsg}</Alert>
    {/if}

    <form on:submit|preventDefault={handleSubmit} class="form">
      <div class="formField">
        <label for="username" class="fieldLabel">Username</label>
        <input
          id="username"
          type="text"
          class="loginInput"
          placeholder="admin"
          bind:value={username}
          disabled={loading}
          autocomplete="username"
        />
      </div>

      <div class="formField">
        <label for="password" class="fieldLabel">Password</label>
        <input
          id="password"
          type="password"
          class="loginInput"
          placeholder="••••••••"
          bind:value={password}
          disabled={loading}
          autocomplete={isSetup ? 'new-password' : 'current-password'}
        />
      </div>

      {#if isSetup}
        <div class="formField">
          <label for="confirmPassword" class="fieldLabel">Confirm Password</label>
          <input
            id="confirmPassword"
            type="password"
            class="loginInput"
            placeholder="••••••••"
            bind:value={confirmPassword}
            disabled={loading}
            autocomplete="new-password"
          />
        </div>
      {/if}

      <div class="formActions">
        <Button kind="primary" size="large" type="submit" disabled={loading}>
          {loading ? 'Authenticating...' : isSetup ? 'Complete Setup' : 'Sign In'}
        </Button>
      </div>

      <div class="toggleMode">
        {#if isSetup}
          <button type="button" class="linkBtn" on:click={() => (isSetup = false)}>
            Already have an account? Sign In
          </button>
        {:else}
          <button type="button" class="linkBtn" on:click={() => (isSetup = true)}>
            First time setup? Create Administrator
          </button>
        {/if}
      </div>
    </form>
    <p class="toggleMode"><a href="{getBaseUrl()}/legal" target="_blank" rel="noopener noreferrer">License and source</a></p>
  </div>
</div>

<style>
  .loginOverlay {
    position: fixed;
    inset: 0;
    z-index: 2000;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: var(--pageBackground);
    padding: 20px;
  }

  .loginCard {
    width: 100%;
    max-width: 440px;
    background-color: var(--cardCenterBackgroundColor);
    border-radius: 6px;
    padding: 35px 30px;
    box-shadow: 0 4px 25px rgba(0, 0, 0, 0.7);
    border: 1px solid rgba(255, 255, 255, 0.08);
  }

  .cardHeader {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    margin-bottom: 25px;
  }

  .logo {
    display: block;
    margin: 0 auto 15px;
    width: 48px;
    height: 48px;
    object-fit: contain;
  }

  .title {
    margin: 0 0 6px 0;
    font-size: 22px;
    font-weight: 400;
    color: var(--white);
  }

  .subtitle {
    margin: 0;
    font-size: var(--defaultFontSize);
    color: var(--helpTextColor);
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .formField {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .fieldLabel {
    font-size: var(--extraSmallFontSize);
    font-weight: bold;
    color: var(--textColor);
    text-transform: uppercase;
  }

  .loginInput {
    height: var(--inputHeight);
    padding: 6px 14px;
    border: 1px solid var(--inputBorderColor);
    border-radius: 4px;
    background-color: var(--inputBackgroundColor);
    color: var(--textColor);
    font-family: inherit;
    font-size: var(--defaultFontSize);
    outline: none;
    box-sizing: border-box;
    width: 100%;
  }

  .loginInput:focus {
    border-color: var(--inputFocusBorderColor);
    box-shadow: inset 0 1px 1px var(--inputBoxShadowColor), 0 0 8px var(--inputFocusBoxShadowColor);
  }

  .formActions {
    margin-top: 10px;
    display: flex;
    flex-direction: column;
  }

  .formActions :global(button) {
    width: 100%;
  }

  .toggleMode {
    text-align: center;
    margin-top: 10px;
  }

  .linkBtn {
    background: transparent;
    border: none;
    color: var(--sonarrBlue);
    cursor: pointer;
    font-size: var(--smallFontSize);
  }

  .linkBtn:hover {
    text-decoration: underline;
  }
</style>
