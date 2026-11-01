<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  export let isOpen = false;
  export let title = '';
  export let size: 'small' | 'medium' | 'large' = 'medium';

  const dispatch = createEventDispatcher<{ close: void }>();

  let modalElement: HTMLElement | null = null;
  let previousActiveElement: HTMLElement | null = null;

  function handleClose() {
    dispatch('close');
  }

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      handleClose();
    }
  }

  function trapFocus(e: KeyboardEvent) {
    if (!isOpen || e.key !== 'Tab' || !modalElement) return;
    const focusable = modalElement.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
    );
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];

    if (e.shiftKey) {
      if (document.activeElement === first) {
        e.preventDefault();
        last.focus();
      }
    } else {
      if (document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isOpen) {
      handleClose();
    } else if (e.key === 'Tab') {
      trapFocus(e);
    }
  }

  $: if (isOpen) {
    if (typeof document !== 'undefined') {
      previousActiveElement = document.activeElement as HTMLElement;
      setTimeout(() => {
        if (modalElement) {
          const first = modalElement.querySelector<HTMLElement>(
            'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
          );
          first?.focus();
        }
      }, 0);
    }
  } else if (previousActiveElement) {
    previousActiveElement.focus();
    previousActiveElement = null;
  }
</script>

<svelte:window on:keydown={handleKeydown} />

{#if isOpen}
  <div class="modalBackdrop" on:click={handleBackdropClick} role="presentation">
    <div class="modal {size}" role="dialog" aria-modal="true" bind:this={modalElement}>
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
    flex-direction: column;
    max-width: 90vw;
    max-height: 90vh;
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
    flex: 1 1 auto;
    width: 100%;
    min-height: 0;
    max-height: 90vh;
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
    flex-shrink: 0;
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
    flex: 1 1 auto;
    min-height: 0;
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
    flex-shrink: 0;
  }
</style>
