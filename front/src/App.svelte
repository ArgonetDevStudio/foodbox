<script>
  import { onMount } from 'svelte';

  const WEEKDAYS = ['일', '월', '화', '수', '목', '금', '토'];
  const MONTH_LABELS = [
    'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
    'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'
  ];

  let menuData = [];
  let today = new Date();
  let year = today.getFullYear();
  let month = today.getMonth();
  let daysInMonth = [];
  let showDatePicker = false;
  let pickerYear = today.getFullYear();
  let loading = true;
  let theme = 'light';

  const THEME_KEY = 'foodbox-theme';

  function applyTheme(next) {
    theme = next;
    document.documentElement.setAttribute('data-theme', next);
    try {
      localStorage.setItem(THEME_KEY, next);
    } catch (_) {
      /* storage may be unavailable */
    }
  }

  function initTheme() {
    let stored = null;
    try {
      stored = localStorage.getItem(THEME_KEY);
    } catch (_) {
      /* ignore */
    }
    if (stored === 'light' || stored === 'dark') {
      applyTheme(stored);
    } else {
      const prefersDark =
        window.matchMedia &&
        window.matchMedia('(prefers-color-scheme: dark)').matches;
      applyTheme(prefersDark ? 'dark' : 'light');
    }
  }

  function toggleTheme() {
    applyTheme(theme === 'dark' ? 'light' : 'dark');
  }

  async function fetchMenuData() {
    loading = true;
    try {
      const response = await fetch('/api/menu');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const data = await response.json();
      menuData = data.data || [];
    } catch (error) {
      console.error('Could not fetch menu data:', error);
    } finally {
      loading = false;
      generateCalendar();
    }
  }

  function getMenuForDate(targetYear, targetMonth, targetDay) {
    const menu = menuData.find((item) => {
      const itemDate = new Date(item.date);
      return (
        itemDate.getFullYear() === targetYear &&
        itemDate.getMonth() === targetMonth &&
        itemDate.getDate() === targetDay
      );
    });
    return menu ? menu.menus : [];
  }

  function generateCalendar() {
    const date = new Date(year, month, 1);
    const firstDay = date.getDay();
    const daysInCurrentMonth = new Date(year, month + 1, 0).getDate();
    const daysInPrevMonth = new Date(year, month, 0).getDate();

    const cells = [];

    for (let i = firstDay; i > 0; i--) {
      const prevMonthDay = daysInPrevMonth - i + 1;
      const d = new Date(year, month - 1, prevMonthDay);
      cells.push({
        day: prevMonthDay,
        weekday: d.getDay(),
        isCurrentMonth: false,
        menus: getMenuForDate(d.getFullYear(), d.getMonth(), d.getDate())
      });
    }

    for (let i = 1; i <= daysInCurrentMonth; i++) {
      const d = new Date(year, month, i);
      cells.push({
        day: i,
        weekday: d.getDay(),
        isCurrentMonth: true,
        menus: getMenuForDate(year, month, i)
      });
    }

    const remainingInLastWeek = 7 - (cells.length % 7);
    if (remainingInLastWeek < 7) {
      for (let i = 1; i <= remainingInLastWeek; i++) {
        const d = new Date(year, month + 1, i);
        cells.push({
          day: i,
          weekday: d.getDay(),
          isCurrentMonth: false,
          menus: getMenuForDate(d.getFullYear(), d.getMonth(), d.getDate())
        });
      }
    }

    daysInMonth = cells;
  }

  function isToday(day) {
    return (
      day === today.getDate() &&
      month === today.getMonth() &&
      year === today.getFullYear()
    );
  }

  $: isCurrentMonthView =
    year === today.getFullYear() && month === today.getMonth();

  function prevMonth() {
    month--;
    if (month < 0) {
      month = 11;
      year--;
    }
    generateCalendar();
  }

  function nextMonth() {
    month++;
    if (month > 11) {
      month = 0;
      year++;
    }
    generateCalendar();
  }

  function goToday() {
    year = today.getFullYear();
    month = today.getMonth();
    generateCalendar();
  }

  function toggleDatePicker() {
    showDatePicker = !showDatePicker;
    if (showDatePicker) {
      pickerYear = year;
    }
  }

  function selectDate(newYear, newMonth) {
    year = newYear;
    month = newMonth;
    showDatePicker = false;
    generateCalendar();
  }

  function formatMonth(monthIndex) {
    return String(monthIndex + 1).padStart(2, '0');
  }

  function prevPickerYear() {
    pickerYear--;
  }

  function nextPickerYear() {
    pickerYear++;
  }

  function handleKeydown(event) {
    if (event.key === 'Escape' && showDatePicker) {
      showDatePicker = false;
    }
  }

  onMount(() => {
    initTheme();
    fetchMenuData();
  });
</script>

<svelte:window on:keydown={handleKeydown} />

<main>
  <div class="calendar-card">
    <header class="cal-header">
      <div class="title-block">
        <div class="brand">
          <span class="brand-emoji">🍱</span>
          <span class="brand-text">이소도시락 점심 메뉴</span>
        </div>
        <button class="month-title" on:click={toggleDatePicker} aria-haspopup="dialog">
          <span class="month-name">{MONTH_LABELS[month]}</span>
          <span class="year-name">{year}</span>
          <svg class="chevron-down" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>

      <div class="controls">
        {#if !isCurrentMonthView}
          <button class="today-button" on:click={goToday}>오늘</button>
        {/if}
        <button
          class="theme-toggle"
          on:click={toggleTheme}
          aria-label={theme === 'dark' ? '라이트 모드로 전환' : '다크 모드로 전환'}
          title={theme === 'dark' ? '라이트 모드' : '다크 모드'}
        >
          {#if theme === 'dark'}
            <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
              <circle cx="12" cy="12" r="4.2" fill="none" stroke="currentColor" stroke-width="2" />
              <path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M18.4 5.6L17 7M7 17l-1.4 1.4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          {:else}
            <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
              <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          {/if}
        </button>
        <div class="nav-group">
          <button class="nav-button" on:click={prevMonth} aria-label="이전 달">
            <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
              <path d="M15 6l-6 6 6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
          <button class="nav-button" on:click={nextMonth} aria-label="다음 달">
            <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
              <path d="M9 6l6 6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </div>
    </header>

    <div class="weekday-row">
      {#each WEEKDAYS as name, i}
        <div class="weekday" class:sun={i === 0} class:sat={i === 6}>{name}</div>
      {/each}
    </div>

    <div class="days-grid" class:loading>
      {#each daysInMonth as dayInfo}
        <div
          class="day"
          class:today={dayInfo.isCurrentMonth && isToday(dayInfo.day)}
          class:other-month={!dayInfo.isCurrentMonth}
          class:has-menu={dayInfo.menus.length > 0}
        >
          <div class="day-top">
            <span
              class="day-number"
              class:sun={dayInfo.weekday === 0}
              class:sat={dayInfo.weekday === 6}
            >{dayInfo.day}</span>
          </div>
          {#if dayInfo.menus.length > 0}
            <ul class="menu-list">
              {#each dayInfo.menus as menuItem}
                <li>{menuItem}</li>
              {/each}
            </ul>
          {/if}
        </div>
      {/each}
    </div>

    {#if loading}
      <div class="overlay-hint">메뉴를 불러오는 중…</div>
    {/if}
  </div>
</main>

{#if showDatePicker}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="picker-overlay" on:click={toggleDatePicker}>
    <div class="picker" role="dialog" aria-modal="true" tabindex="-1" on:click|stopPropagation>
      <div class="picker-header">
        <h3>연월 선택</h3>
        <button class="close-button" on:click={toggleDatePicker} aria-label="닫기">
          <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
      </div>

      <div class="year-selector">
        <button class="year-nav" on:click={prevPickerYear} aria-label="이전 해">
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path d="M15 6l-6 6 6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <span class="current-year">{pickerYear}</span>
        <button class="year-nav" on:click={nextPickerYear} aria-label="다음 해">
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path d="M9 6l6 6-6 6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>

      <div class="month-grid">
        {#each Array(12) as _, monthIndex}
          <button
            class="month-button"
            class:active={pickerYear === year && monthIndex === month}
            class:is-today={pickerYear === today.getFullYear() && monthIndex === today.getMonth()}
            on:click={() => selectDate(pickerYear, monthIndex)}
          >
            {MONTH_LABELS[monthIndex]}
          </button>
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  main {
    width: 100%;
    max-width: 1360px;
    margin: 0 auto;
    padding: clamp(16px, 4vw, 48px);
  }

  .calendar-card {
    position: relative;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: clamp(20px, 3vw, 36px);
  }

  /* ---------- Header ---------- */
  .cal-header {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 20px;
    flex-wrap: wrap;
    margin-bottom: 28px;
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--text-muted);
    margin-bottom: 8px;
  }

  .brand-emoji {
    font-size: 1.1rem;
  }

  .month-title {
    display: inline-flex;
    align-items: baseline;
    gap: 12px;
    background: transparent;
    border: none;
    padding: 4px 6px;
    margin: -4px -6px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    color: var(--text);
    transition: background 0.2s var(--ease);
  }

  .month-title:hover {
    background: var(--surface-muted);
  }

  .month-name {
    font-size: clamp(1.9rem, 4vw, 2.6rem);
    font-weight: 800;
    letter-spacing: -0.02em;
    line-height: 1;
  }

  .year-name {
    font-size: clamp(1.1rem, 2.5vw, 1.5rem);
    font-weight: 600;
    color: var(--text-faint);
    letter-spacing: -0.01em;
  }

  .chevron-down {
    align-self: center;
    color: var(--text-faint);
    transition: transform 0.2s var(--ease), color 0.2s var(--ease);
  }

  .month-title:hover .chevron-down {
    color: var(--accent);
    transform: translateY(2px);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .today-button {
    height: 44px;
    padding: 0 18px;
    border-radius: 999px;
    border: 1px solid var(--border-strong);
    background: var(--surface);
    color: var(--text);
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.2s var(--ease);
  }

  .today-button:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-soft);
  }

  .theme-toggle {
    width: 44px;
    height: 44px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    border: 1px solid var(--border-strong);
    background: var(--surface);
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.2s var(--ease);
  }

  .theme-toggle:hover {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-soft);
    transform: rotate(-15deg);
  }

  .theme-toggle:active {
    transform: scale(0.92);
  }

  .nav-group {
    display: inline-flex;
    gap: 6px;
    padding: 5px;
    background: var(--surface-muted);
    border: 1px solid var(--border);
    border-radius: 999px;
  }

  .nav-button {
    width: 40px;
    height: 40px;
    display: grid;
    place-items: center;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.18s var(--ease);
  }

  .nav-button:hover {
    background: var(--surface);
    color: var(--accent);
    box-shadow: var(--shadow-sm);
  }

  .nav-button:active {
    transform: scale(0.92);
  }

  /* ---------- Weekday row ---------- */
  .weekday-row {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 10px;
    margin-bottom: 10px;
  }

  .weekday {
    text-align: center;
    font-size: 0.88rem;
    font-weight: 700;
    letter-spacing: 0.02em;
    color: var(--text-muted);
    padding: 6px 0;
  }

  .weekday.sun {
    color: var(--sun);
  }
  .weekday.sat {
    color: var(--sat);
  }

  /* ---------- Days grid ---------- */
  .days-grid {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 10px;
    transition: opacity 0.2s var(--ease);
  }

  .days-grid.loading {
    opacity: 0.4;
    pointer-events: none;
  }

  .day {
    position: relative;
    display: flex;
    flex-direction: column;
    min-height: 144px;
    padding: 13px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    transition: transform 0.18s var(--ease), box-shadow 0.18s var(--ease),
      border-color 0.18s var(--ease);
  }

  .day.has-menu:hover {
    transform: translateY(-3px);
    box-shadow: var(--shadow-md);
    border-color: var(--border-strong);
  }

  .day.other-month {
    background: transparent;
    border-color: transparent;
  }

  .day.other-month .day-number {
    color: var(--text-faint);
    opacity: 0.55;
  }

  .day-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  .day-number {
    display: inline-grid;
    place-items: center;
    min-width: 30px;
    height: 30px;
    padding: 0 7px;
    font-size: 1rem;
    font-weight: 700;
    color: var(--text);
    border-radius: 999px;
  }

  .day-number.sun {
    color: var(--sun);
  }
  .day-number.sat {
    color: var(--sat);
  }

  /* Today */
  .day.today {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent), var(--shadow-md);
    background: linear-gradient(180deg, var(--accent-soft), transparent 60%),
      var(--surface);
  }

  .day.today .day-number {
    color: var(--accent-contrast);
    background: var(--accent);
    box-shadow: 0 2px 8px rgba(242, 84, 45, 0.35);
  }

  /* Menu list */
  .menu-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }

  .menu-list::-webkit-scrollbar {
    width: 5px;
  }
  .menu-list::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 999px;
  }

  .menu-list li {
    position: relative;
    font-size: 0.95rem;
    line-height: 1.5;
    font-weight: 500;
    color: var(--text-body);
    letter-spacing: -0.01em;
    padding: 3px 6px 3px 16px;
    border-radius: 7px;
    word-break: keep-all;
  }

  .menu-list li::before {
    content: '';
    position: absolute;
    left: 5px;
    top: 0.68em;
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
  }

  .day.today .menu-list li {
    color: var(--text);
    font-weight: 600;
  }

  /* Loading hint */
  .overlay-hint {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    padding: 12px 20px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 999px;
    box-shadow: var(--shadow-md);
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--text-muted);
  }

  /* ---------- Date picker ---------- */
  .picker-overlay {
    position: fixed;
    inset: 0;
    background: rgba(15, 18, 24, 0.45);
    backdrop-filter: blur(6px);
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 12vh 20px 20px;
    z-index: 1000;
    animation: fade 0.18s var(--ease);
  }

  .picker {
    width: 100%;
    max-width: 380px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 24px;
    animation: pop 0.22s var(--ease);
  }

  .picker-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
  }

  .picker-header h3 {
    margin: 0;
    font-size: 1.1rem;
    font-weight: 700;
    color: var(--text);
  }

  .close-button {
    width: 34px;
    height: 34px;
    display: grid;
    place-items: center;
    border: none;
    border-radius: 50%;
    background: var(--surface-muted);
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.18s var(--ease);
  }

  .close-button:hover {
    background: var(--accent-soft);
    color: var(--accent);
  }

  .year-selector {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 16px;
    margin-bottom: 20px;
  }

  .year-nav {
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    border: 1px solid var(--border);
    border-radius: 50%;
    background: var(--surface);
    color: var(--text-muted);
    cursor: pointer;
    transition: all 0.18s var(--ease);
  }

  .year-nav:hover {
    border-color: var(--accent);
    color: var(--accent);
  }

  .current-year {
    font-size: 1.3rem;
    font-weight: 700;
    color: var(--text);
    min-width: 4.5rem;
    text-align: center;
  }

  .month-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }

  .month-button {
    padding: 12px 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text-muted);
    font-size: 0.92rem;
    font-weight: 600;
    cursor: pointer;
    transition: all 0.16s var(--ease);
  }

  .month-button:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-soft);
  }

  .month-button.is-today {
    color: var(--accent);
  }

  .month-button.active {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-contrast);
    box-shadow: 0 4px 12px rgba(242, 84, 45, 0.3);
  }

  @keyframes fade {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes pop {
    from { opacity: 0; transform: translateY(-8px) scale(0.98); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  /* ---------- Responsive ---------- */
  @media (max-width: 720px) {
    .weekday-row,
    .days-grid {
      gap: 6px;
    }

    .day {
      min-height: 96px;
      padding: 8px;
      border-radius: var(--radius-sm);
    }

    .day-number {
      min-width: 24px;
      height: 24px;
      font-size: 0.82rem;
    }

    .menu-list li {
      font-size: 0.84rem;
      padding: 2px 4px 2px 14px;
    }

    .menu-list li::before {
      left: 4px;
    }

    .cal-header {
      align-items: center;
    }
  }

  @media (max-width: 480px) {
    .day {
      min-height: 82px;
    }
    .menu-list li {
      font-size: 0.8rem;
    }
  }
</style>
