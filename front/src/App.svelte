
<script>
  import { onMount } from 'svelte';

  let menuData = [];
  let today = new Date();
  let year = today.getFullYear();
  let month = today.getMonth();
  let daysInMonth = [];
  let monthNames = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

  async function fetchMenuData() {
    try {
      const response = await fetch('/api/menu');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
      const data = await response.json();
      menuData = data.data || [];
      generateCalendar();
    } catch (error) {
      console.error("Could not fetch menu data:", error);
    }
  }

  function getMenuForDate(targetYear, targetMonth, targetDay) {
    const menu = menuData.find(item => {
      const itemDate = new Date(item.date);
      return itemDate.getFullYear() === targetYear && itemDate.getMonth() === targetMonth && itemDate.getDate() === targetDay;
    });
    return menu ? menu.menus : [];
  }

  function generateCalendar() {
    const date = new Date(year, month, 1);
    const firstDay = date.getDay(); // 0 for Sunday, 6 for Saturday
    const daysInCurrentMonth = new Date(year, month + 1, 0).getDate();
    const daysInPrevMonth = new Date(year, month, 0).getDate();
    
    daysInMonth = [];

    // Add days from previous month
    for (let i = firstDay; i > 0; i--) {
      const prevMonthDay = daysInPrevMonth - i + 1;
      const prevMonthDate = new Date(year, month - 1, prevMonthDay);
      daysInMonth.push({
        day: prevMonthDay,
        isCurrentMonth: false,
        menus: getMenuForDate(prevMonthDate.getFullYear(), prevMonthDate.getMonth(), prevMonthDate.getDate())
      });
    }

    // Add days from current month
    for (let i = 1; i <= daysInCurrentMonth; i++) {
      daysInMonth.push({
        day: i,
        isCurrentMonth: true,
        menus: getMenuForDate(year, month, i)
      });
    }

    // Add days from next month to complete the last week only
    const currentTotal = daysInMonth.length;
    const remainingInLastWeek = 7 - (currentTotal % 7);
    if (remainingInLastWeek < 7) {
      for (let i = 1; i <= remainingInLastWeek; i++) {
        const nextMonthDate = new Date(year, month + 1, i);
        daysInMonth.push({
          day: i,
          isCurrentMonth: false,
          menus: getMenuForDate(nextMonthDate.getFullYear(), nextMonthDate.getMonth(), nextMonthDate.getDate())
        });
      }
    }
  }

  function isToday(day) {
    return day === today.getDate() && month === today.getMonth() && year === today.getFullYear();
  }

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

  onMount(() => {
    fetchMenuData();
  });
</script>

<main>
  <div class="calendar-container">
    <div class="calendar-header">
      <button on:click={prevMonth} class="nav-button">‹</button>
      <h1 class="month-title">{monthNames[month]} {year}</h1>
      <button on:click={nextMonth} class="nav-button">›</button>
    </div>
    <div class="days-grid">
      <div class="day-name">Sun</div>
      <div class="day-name">Mon</div>
      <div class="day-name">Tue</div>
      <div class="day-name">Wed</div>
      <div class="day-name">Thu</div>
      <div class="day-name">Fri</div>
      <div class="day-name">Sat</div>
      {#each daysInMonth as dayInfo}
        <div class="day" class:today={dayInfo && dayInfo.isCurrentMonth && isToday(dayInfo.day)} class:other-month={!dayInfo.isCurrentMonth}>
          <div class="day-number">{dayInfo.day}</div>
          <ul class="menu-list">
            {#each dayInfo.menus as menuItem}
              <li>{menuItem}</li>
            {/each}
          </ul>
        </div>
      {/each}
    </div>
  </div>
</main>

<style>
  :global(body) {
    background: #000000;
    font-family: 'Courier New', monospace;
    display: flex;
    justify-content: center;
    align-items: center;
    min-height: 100vh;
    margin: 0;
    padding: 20px;
    box-sizing: border-box;
    position: relative;
    overflow-x: hidden;
  }

  :global(body::before) {
    content: '';
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: 
      repeating-linear-gradient(
        0deg,
        transparent,
        transparent 2px,
        rgba(255, 255, 255, 0.02) 2px,
        rgba(255, 255, 255, 0.02) 4px
      );
    pointer-events: none;
    z-index: -1;
  }

  main {
    width: 100%;
    max-width: 1400px;
    perspective: 1000px;
  }

  .calendar-container {
    width: 100%;
    background: #000000;
    border-radius: 0;
    box-shadow: 
      inset 0 0 0 2px #ffffff,
      0 0 20px rgba(255, 255, 255, 0.3);
    display: flex;
    flex-direction: column;
    border: 2px solid #ffffff;
    padding: 32px;
    transform: none;
    transition: all 0.3s ease;
    position: relative;
    font-family: 'Courier New', monospace;
  }

  .calendar-container::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: 
      repeating-linear-gradient(
        90deg,
        transparent,
        transparent 1px,
        rgba(255, 255, 255, 0.05) 1px,
        rgba(255, 255, 255, 0.05) 2px
      );
    z-index: -1;
  }

  .calendar-container:hover {
    transform: none;
    box-shadow: 
      inset 0 0 0 2px #ffffff,
      0 0 30px rgba(255, 255, 255, 0.5);
  }

  .calendar-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 2rem;
    padding-bottom: 1.5rem;
    border-bottom: 2px solid #ffffff;
    background: rgba(255, 255, 255, 0.05);
    border-radius: 0;
    padding: 1.5rem;
    position: relative;
    overflow: hidden;
    border: 1px solid #ffffff;
  }

  .calendar-header::before {
    content: '> CALENDAR.EXE';
    position: absolute;
    top: -25px;
    left: 0;
    width: 100%;
    height: 20px;
    background: #000000;
    color: #ffffff;
    font-family: 'Courier New', monospace;
    font-size: 12px;
    padding: 2px 8px;
    border: 1px solid #ffffff;
  }

  .month-title {
    font-size: 2.8rem;
    font-weight: 700;
    color: #ffffff;
    font-family: 'Courier New', monospace;
    margin: 0;
    letter-spacing: 0.1em;
    text-shadow: 0 0 10px rgba(255, 255, 255, 0.8);
    text-transform: uppercase;
  }

  .nav-button {
    background: #000000;
    border: 2px solid #ffffff;
    color: #ffffff;
    font-size: 1.8rem;
    font-weight: 600;
    cursor: pointer;
    border-radius: 0;
    width: 56px;
    height: 56px;
    display: flex;
    justify-content: center;
    align-items: center;
    transition: all 0.3s ease;
    box-shadow: none;
    position: relative;
    overflow: hidden;
    font-family: 'Courier New', monospace;
  }

  .nav-button::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: rgba(255, 255, 255, 0.2);
    opacity: 0;
    transition: opacity 0.3s ease;
  }

  .nav-button:hover {
    transform: none;
    box-shadow: 0 0 15px rgba(255, 255, 255, 0.8);
    color: #000000;
    background: #ffffff;
  }

  .nav-button:hover::before {
    opacity: 1;
  }

  .nav-button:active {
    transform: translateY(0) scale(0.98);
  }

  .days-grid {
    flex-grow: 1;
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    grid-template-rows: auto;
    grid-auto-rows: 1fr;
    gap: 16px;
  }

  .day-name {
    font-weight: 700;
    font-size: 0.9rem;
    color: #ffffff;
    text-align: center;
    padding: 1rem 0;
    background: #000000;
    border: 1px solid #ffffff;
    border-radius: 0;
    margin-bottom: 8px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    box-shadow: none;
    font-family: 'Courier New', monospace;
  }

  .day {
    background: #000000;
    border-radius: 0;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    border: 1px solid #ffffff;
    transition: all 0.3s ease;
    min-height: 140px;
    position: relative;
    overflow: hidden;
    box-shadow: none;
    font-family: 'Courier New', monospace;
  }

  .day.other-month {
    opacity: 0.3;
  }

  .day:hover {
    transform: none;
    box-shadow: 0 0 15px rgba(255, 255, 255, 0.5);
    border-color: #ffffff;
  }

  .day-number {
    font-weight: 700;
    color: #ffffff;
    margin-bottom: 0.75rem;
    font-size: 1.1rem;
    text-align: center;
    position: relative;
    z-index: 2;
    font-family: 'Courier New', monospace;
  }

  .day.today {
    background: rgba(255, 255, 255, 0.2);
    color: #ffffff;
    box-shadow: 
      0 0 20px rgba(255, 255, 255, 0.8),
      inset 0 0 10px rgba(255, 255, 255, 0.3);
    border-color: #ffffff;
    border-width: 3px;
  }

  .day.today::before {
    opacity: 0;
  }

  .day.today .day-number {
    color: #000000;
    background: #ffffff;
    border-radius: 0;
    width: 2.5em;
    height: 2.5em;
    display: flex;
    justify-content: center;
    align-items: center;
    font-weight: 800;
    margin: 0 auto 0.75rem;
    backdrop-filter: none;
    box-shadow: 0 0 10px rgba(255, 255, 255, 0.8);
    font-family: 'Courier New', monospace;
    border: 2px solid #ffffff;
  }

  .day.today .menu-list {
    color: #ffffff;
  }

  .day.today .menu-list li {
    background: rgba(255, 255, 255, 0.3);
    padding: 0.25rem 0.5rem;
    border-radius: 0;
    margin-bottom: 0.3rem;
    backdrop-filter: none;
    color: #ffffff;
    font-weight: 600;
    border: 1px solid #ffffff;
  }

  .menu-list {
    list-style: none;
    padding: 0;
    margin: 0;
    font-size: 0.85rem;
    line-height: 1.5;
    color: #ffffff;
    overflow-y: auto;
    font-weight: 500;
    font-family: 'Courier New', monospace;
  }

  .menu-list li {
    margin-bottom: 0.3rem;
    padding: 0.25rem 0.5rem;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 0;
    transition: all 0.2s ease;
    border-left: 2px solid transparent;
    border: 1px solid rgba(255, 255, 255, 0.3);
  }

  .menu-list li:hover {
    background: rgba(255, 255, 255, 0.2);
    border-left-color: #ffffff;
    box-shadow: 0 0 5px rgba(255, 255, 255, 0.5);
  }

  @media (max-width: 768px) {
    :global(body) {
      padding: 12px;
    }
    
    .calendar-container {
      padding: 20px;
      border-radius: 20px;
      transform: none;
    }
    
    .month-title {
      font-size: 2.2rem;
    }
    
    .nav-button {
      width: 48px;
      height: 48px;
      font-size: 1.6rem;
    }
    
    .days-grid {
      gap: 12px;
    }
    
    .day {
      min-height: 120px;
      padding: 0.75rem;
    }
  }
</style>
