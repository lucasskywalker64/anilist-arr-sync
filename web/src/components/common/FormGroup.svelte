<script lang="ts">
  export let label = '';
  export let helpText = '';
  export let isAdvanced = false;
  export let hasError = false;
  export let isDirty = false;
  export let errorMessage = '';
  export let forId: string | undefined = undefined;
</script>

<div class="group">
  {#if forId}
    <label for={forId} class="label {isAdvanced ? 'isAdvanced' : ''} {hasError ? 'hasError' : ''}">
      {label}
    </label>
  {:else}
    <span class="label {isAdvanced ? 'isAdvanced' : ''} {hasError ? 'hasError' : ''}">
      {label}
    </span>
  {/if}
  <div class="inputGroupContainer">
    <div class="inputGroup">
      <slot />
      {#if isDirty}
        <div class="pendingChangesContainer" title="Field has unsaved changes">
          <span class="pendingChangesIcon">&bull;</span>
        </div>
      {/if}
    </div>
    {#if hasError && errorMessage}
      <div class="errorMessage">{errorMessage}</div>
    {/if}
    {#if helpText}
      <div class="helpText">{helpText}</div>
    {/if}
  </div>
</div>

<style>
  .group {
    display: flex;
    margin-bottom: 20px;
    max-width: var(--formGroupMediumWidth);
  }

  .label {
    display: flex;
    justify-content: flex-end;
    flex: 0 0 var(--formLabelLargeWidth);
    margin-right: var(--formLabelRightMarginWidth);
    padding-top: 8px;
    min-height: var(--inputHeight);
    text-align: end;
    font-weight: bold;
    color: var(--textColor);
    user-select: none;
  }

  .label.isAdvanced {
    color: var(--advancedFormLabelColor);
  }

  .label.hasError {
    color: var(--dangerColor);
  }

  .inputGroupContainer {
    flex: 1 1 auto;
    min-width: 0;
  }

  .inputGroup {
    display: flex;
    flex: 1 1 auto;
    flex-wrap: nowrap;
    align-items: center;
  }

  .pendingChangesContainer {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 30px;
  }

  .pendingChangesIcon {
    color: var(--warningColor);
    font-size: 28px;
    line-height: var(--inputHeight);
  }

  .helpText {
    margin-top: 5px;
    color: var(--helpTextColor);
    line-height: 20px;
    font-size: var(--defaultFontSize);
  }

  .errorMessage {
    margin-top: 5px;
    color: var(--dangerColor);
    line-height: 20px;
    font-size: var(--smallFontSize);
  }

  /* Global child styling for inputs within FormGroup */
  :global(.inputGroup input.formInput),
  :global(.inputGroup select.formInput) {
    padding: 6px 16px;
    width: 100%;
    height: var(--inputHeight);
    border: 1px solid var(--inputBorderColor);
    border-radius: 4px;
    background-color: var(--inputBackgroundColor);
    box-shadow: inset 0 1px 1px var(--inputBoxShadowColor);
    color: var(--textColor);
    font-family: inherit;
    font-size: var(--defaultFontSize);
    outline: none;
    transition: border-color 150ms ease, box-shadow 150ms ease;
  }

  :global(.inputGroup input.formInput:focus),
  :global(.inputGroup select.formInput:focus) {
    border-color: var(--inputFocusBorderColor);
    box-shadow: inset 0 1px 1px var(--inputBoxShadowColor), 0 0 8px var(--inputFocusBoxShadowColor);
  }

  :global(.inputGroup input.formInput.hasButton),
  :global(.inputGroup select.formInput.hasButton) {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }

  :global(.inputGroup .formInputButton) {
    height: var(--inputHeight);
    border: 1px solid var(--inputBorderColor);
    border-left: none;
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    border-top-right-radius: 4px;
    border-bottom-right-radius: 4px;
    background-color: var(--defaultBackgroundColor);
    color: var(--defaultButtonTextColor);
    padding: 0 16px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    white-space: nowrap;
  }

  :global(.inputGroup .formInputButton:hover) {
    background-color: var(--defaultHoverBackgroundColor);
  }

  @media (max-width: 1200px) {
    .group {
      display: block;
    }
    .label {
      justify-content: flex-start;
      margin-bottom: 5px;
      min-height: auto;
      text-align: start;
    }
  }
</style>
