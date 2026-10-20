<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  export let isOpen = false;
  export let title = '';
  export let size: 'small' | 'medium' | 'large' = 'medium';

  const dispatch = createEventDispatcher<{ close: void }>();

  function handleClose() {
    dispatch('close');
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      handleClose();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isOpen) {
      handleClose();
    }
  }
</script>

<svelte:window on:keydown={handleKeydown} />

{#if isOpen}
  <div class="modalBackdrop" on:click={handleBackdropClick} role="presentation">
    <div class="modal {size}" role="dialog" aria-modal="true">
      <div class="modalContent">
        <div class="modalHeader">
          <span>{title}</span>
          <button class="modalClose" on:click={handleClose} aria-label="Close modal">
            &times;
          </button>
        </div>
        <div class="modalBody">
          <slot name="body" />
          <slot />
        </div>
        {#if $$slots.footer}
          <div class="modalFooter">
            <slot name="footer" />
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .modalBackdrop {
    position: fixed;
    inset: 0;
    z-index: 1050;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: var(--modalBackdropBackgroundColor);
    backdrop-filter: blur(2px);
  }

  .modal {
    position: relative;
    display: flex;
    max-width: 90%;
    max-height: 90%;
    border-radius: 6px;
    overflow: hidden;
    box-shadow: 0 5px 25px rgba(0, 0, 0, 0.7);
  }

  .small {
    width: 480px;
  }

  .medium {
    width: 720px;
  }

  .large {
    width: 1080px;
  }

  .modalContent {
    display: flex;
    flex-direction: column;
    flex-grow: 1;
    width: 100%;
    background-color: var(--modalBackgroundColor);
    overflow: hidden;
  }

  .modalHeader {
    position: relative;
    padding: 15px 50px 15px 30px;
    border-bottom: 1px solid var(--borderColor);
    font-size: 18px;
    font-weight: bold;
    color: var(--white);
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .modalClose {
    position: absolute;
    right: 20px;
    top: 12px;
    background: transparent;
    border: none;
    color: var(--defaultColor);
    cursor: pointer;
    font-size: 26px;
    line-height: 1;
    padding: 0;
  }

  .modalClose:hover {
    color: var(--modalCloseButtonHoverColor);
  }

  .modalBody {
    flex: 1 0 1px;
    overflow-y: auto;
    padding: var(--modalBodyPadding);
  }

  .modalFooter {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    padding: 15px 30px;
    border-top: 1px solid var(--borderColor);
    background-color: rgba(0, 0, 0, 0.1);
  }
</style>
