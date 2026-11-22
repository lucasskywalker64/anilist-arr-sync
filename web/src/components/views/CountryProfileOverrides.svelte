<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import { Edit, Plus, Trash2 } from "lucide-svelte";
  import FormGroup from "../common/FormGroup.svelte";
  import Button from "../common/Button.svelte";

  export let serviceKind: "sonarr" | "radarr";
  export let serviceName: string;
  export let overrides: Record<string, number> = {};
  export let qualityProfiles: { id: number; name: string }[] = [];
  export let isDirty = false;
  export let resetToken = 0;

  const dispatch = createEventDispatcher<{ change: Record<string, number> }>();

  let draftCountry = "";
  let draftProfileId = "";
  let editing: { originalCountry: string; country: string; profileId: string } | null = null;
  let error = "";
  let lastResetToken = resetToken;

  $: sortedEntries = Object.entries(overrides ?? {}).sort(([a], [b]) => a.localeCompare(b));

  $: if (resetToken !== lastResetToken) {
    lastResetToken = resetToken;
    editing = null;
    error = "";
    draftCountry = "";
    draftProfileId = qualityProfiles.length > 0 ? String(qualityProfiles[0].id) : "";
  }

  $: if (
    qualityProfiles.length > 0 &&
    (!draftProfileId || !qualityProfiles.some((profile) => String(profile.id) === draftProfileId))
  ) {
    draftProfileId = String(qualityProfiles[0].id);
  }

  function normalizeMap(raw: Record<string, number>): Record<string, number> {
    const entries = Object.entries(raw)
      .map(([country, id]) => [country.trim().toUpperCase(), id] as const)
      .sort(([a], [b]) => a.localeCompare(b));
    return Object.fromEntries(entries);
  }

  function validateEntry(
    rawCountry: string,
    rawProfileId: string,
    originalCountry?: string,
  ): { code: string; profileId: number } | null {
    const code = rawCountry.trim().toUpperCase();
    if (!/^[A-Z]{2}$/.test(code)) {
      error = "Enter a valid 2-letter ASCII country code (for example CN, KR, or TW).";
      return null;
    }
    const trimmedId = String(rawProfileId ?? "").trim();
    const profileId = Number(trimmedId);
    if (!trimmedId || !Number.isInteger(profileId) || profileId <= 0) {
      error = "Select or enter a positive quality profile ID.";
      return null;
    }
    if (code !== originalCountry && Object.prototype.hasOwnProperty.call(overrides ?? {}, code)) {
      error = `An override for ${code} already exists. Edit the existing row instead.`;
      return null;
    }
    error = "";
    return { code, profileId };
  }

  function addOverride() {
    const validated = validateEntry(draftCountry, draftProfileId);
    if (!validated) return;
    dispatch("change", normalizeMap({ ...(overrides ?? {}), [validated.code]: validated.profileId }));
    draftCountry = "";
    draftProfileId = qualityProfiles.length > 0 ? String(qualityProfiles[0].id) : "";
  }

  function startEdit(country: string, profileId: number) {
    editing = { originalCountry: country, country, profileId: String(profileId) };
    error = "";
  }

  function saveEdit() {
    if (!editing) return;
    const validated = validateEntry(editing.country, editing.profileId, editing.originalCountry);
    if (!validated) return;
    const next = { ...(overrides ?? {}) };
    if (editing.originalCountry !== validated.code) {
      delete next[editing.originalCountry];
    }
    next[validated.code] = validated.profileId;
    dispatch("change", normalizeMap(next));
    editing = null;
  }

  function cancelEdit() {
    editing = null;
    error = "";
  }

  function removeOverride(country: string) {
    const next = { ...(overrides ?? {}) };
    delete next[country];
    dispatch("change", normalizeMap(next));
    if (editing?.originalCountry === country) {
      editing = null;
    }
    error = "";
  }

  function formatProfileName(profileId: number): string {
    const found = qualityProfiles.find((profile) => profile.id === profileId);
    return found ? found.name : `Profile ID ${profileId}`;
  }
</script>

<FormGroup
  label="Country Profile Overrides"
  helpText="Optional country overrides (e.g. CN, KR, TW) applied only when adding new entries."
  {isDirty}
  hasError={!!error}
  errorMessage={error}
>
  <div class="countryOverridesContainer">
    {#if sortedEntries.length > 0}
      <div class="countryOverridesList">
        {#each sortedEntries as [country, profileId] (country)}
          {#if editing && editing.originalCountry === country}
            <div class="countryOverrideRow">
              <input
                id={`${serviceKind}-edit-override-country`}
                type="text"
                maxlength="2"
                list="anilist-country-codes"
                class="formInput countryCodeInput"
                placeholder="CN"
                value={editing.country}
                on:input={(e) => {
                  if (editing) editing = { ...editing, country: e.currentTarget.value };
                }}
              />
              {#if qualityProfiles.length > 0}
                <select
                  id={`${serviceKind}-edit-override-profile`}
                  class="formInput overrideProfileInput"
                  value={editing.profileId}
                  on:change={(e) => {
                    if (editing) editing = { ...editing, profileId: e.currentTarget.value };
                  }}
                >
                  {#if !qualityProfiles.some((profile) => String(profile.id) === editing?.profileId)}
                    <option value={editing.profileId}>Current ID ({editing.profileId})</option>
                  {/if}
                  {#each qualityProfiles as profile}
                    <option value={String(profile.id)}>{profile.name}</option>
                  {/each}
                </select>
              {:else}
                <input
                  id={`${serviceKind}-edit-override-profile`}
                  type="number"
                  min="1"
                  step="1"
                  class="formInput overrideProfileInput"
                  placeholder="Profile ID"
                  value={editing.profileId}
                  on:input={(e) => {
                    if (editing) editing = { ...editing, profileId: e.currentTarget.value };
                  }}
                />
              {/if}
              <div class="overrideRowActions">
                <Button
                  kind="primary"
                  size="small"
                  ariaLabel={`Save ${serviceName} override edit`}
                  on:click={saveEdit}
                >
                  Save
                </Button>
                <Button
                  kind="default"
                  size="small"
                  ariaLabel={`Cancel ${serviceName} override edit`}
                  on:click={cancelEdit}
                >
                  Cancel
                </Button>
              </div>
            </div>
          {:else}
            <div class="countryOverrideRow">
              <span class="countryCodeBadge">{country}</span>
              <span class="overrideProfileLabel">{formatProfileName(profileId)}</span>
              <div class="overrideRowActions">
                <button
                  type="button"
                  class="overrideIconBtn"
                  title={`Edit ${country} override`}
                  aria-label={`Edit ${serviceName} override ${country}`}
                  on:click={() => startEdit(country, profileId)}
                >
                  <Edit size={15} />
                </button>
                <button
                  type="button"
                  class="overrideIconBtn deleteBtn"
                  title={`Remove ${country} override`}
                  aria-label={`Remove ${serviceName} override ${country}`}
                  on:click={() => removeOverride(country)}
                >
                  <Trash2 size={15} />
                </button>
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}

    <div class="countryOverrideRow">
      <input
        id={`${serviceKind}-override-country`}
        type="text"
        maxlength="2"
        list="anilist-country-codes"
        class="formInput countryCodeInput"
        placeholder="CN"
        value={draftCountry}
        on:input={(e) => (draftCountry = e.currentTarget.value)}
      />
      {#if qualityProfiles.length > 0}
        <select
          id={`${serviceKind}-override-profile`}
          class="formInput overrideProfileInput"
          value={draftProfileId}
          on:change={(e) => (draftProfileId = e.currentTarget.value)}
        >
          <option value="">Select quality profile...</option>
          {#each qualityProfiles as profile}
            <option value={String(profile.id)}>{profile.name}</option>
          {/each}
        </select>
      {:else}
        <input
          id={`${serviceKind}-override-profile`}
          type="number"
          min="1"
          step="1"
          class="formInput overrideProfileInput"
          placeholder="Profile ID"
          value={draftProfileId}
          on:input={(e) => (draftProfileId = e.currentTarget.value)}
        />
      {/if}
      <Button
        kind="default"
        size="small"
        ariaLabel={`Add ${serviceName} country override`}
        on:click={addOverride}
      >
        <Plus size={14} /> Add
      </Button>
    </div>
  </div>
</FormGroup>

<style>
  .countryOverridesContainer {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }

  .countryOverridesList {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .countryOverrideRow {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
  }

  .countryCodeBadge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 44px;
    height: var(--inputHeight);
    padding: 0 8px;
    border-radius: 4px;
    background-color: var(--cardBackgroundColor);
    border: 1px solid var(--inputBorderColor);
    color: var(--white);
    font-weight: 600;
    font-size: var(--smallFontSize);
    letter-spacing: 0.04em;
  }

  .overrideProfileLabel {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--textColor);
    font-size: var(--defaultFontSize);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :global(.inputGroup input.formInput.countryCodeInput) {
    width: 72px;
    flex: 0 0 72px;
    text-transform: uppercase;
    text-align: center;
    padding: 6px 8px;
  }

  :global(.inputGroup input.formInput.overrideProfileInput),
  :global(.inputGroup select.formInput.overrideProfileInput) {
    flex: 1 1 auto;
    min-width: 0;
  }

  .overrideRowActions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 0 0 auto;
  }

  .overrideIconBtn {
    background: transparent;
    border: 1px solid transparent;
    color: var(--textColor);
    cursor: pointer;
    padding: 6px;
    border-radius: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: color 150ms ease, background-color 150ms ease;
  }

  .overrideIconBtn:hover {
    color: var(--themeBlue);
    background-color: var(--toolbarMenuItemHoverBackgroundColor);
  }

  .deleteBtn:hover {
    color: var(--dangerColor);
  }
</style>
