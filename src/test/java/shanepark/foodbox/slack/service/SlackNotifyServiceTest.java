package shanepark.foodbox.slack.service;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.service.MenuService;
import shanepark.foodbox.slack.SlackConfig;
import shanepark.foodbox.slack.domain.SlackPayload;

import java.io.IOException;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.ZoneId;
import java.util.List;

import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class SlackNotifyServiceTest {

    @InjectMocks
    SlackNotifyService slackNotifyService;

    @Mock
    private SlackMessageSender slackMessageSender;

    @Mock
    SlackConfig slackConfig;

    @Mock
    MenuService menuService;

    @Mock
    Clock clock;

    LocalDate monday = LocalDate.of(2025, 3, 31);

    void mockClock(LocalDate date) {
        ZoneId zoneId = ZoneId.systemDefault();
        Instant fixedInstant = date.atStartOfDay(zoneId).toInstant();
        when(clock.instant()).thenReturn(fixedInstant);
        when(clock.getZone()).thenReturn(zoneId);
    }

    @Test
    void notifyTodayMenu() throws IOException, InterruptedException {
        // Given
        String slackToken = "SLACK_TOKEN_HERE_FOR_REAL_TEST";
        SlackConfig slackConfig = new SlackConfig("foodbox", "https://hooks.slack.com/services", slackToken, "점심봇");
        SlackMessageSender slackMessageSender = new SlackMessageSender();
        SlackNotifyService slackNotifyService = new SlackNotifyService(menuService, slackMessageSender, slackConfig, clock);
        mockClock(monday);

        // When
        when(menuService.getTodayMenu(monday)).thenReturn(new MenuResponse(LocalDate.of(2024, 11, 8), List.of("김치찌개", "된장찌개", "제육볶음")));

        // Then
        slackNotifyService.notifyTodayMenu();
    }

    @Test
    @DisplayName("invalid menu(with no line) should not send message")
    void shouldNotSendMessageWhenMenuIsInvalid() throws IOException, InterruptedException {
        MenuResponse invalidMenu1 = new MenuResponse(LocalDate.now(), List.of(""));
        when(menuService.getTodayMenu(monday)).thenReturn(invalidMenu1);
        mockClock(monday);

        // when
        slackNotifyService.notifyTodayMenu();

        // then
        verify(slackMessageSender, never()).sendMessage(anyString(), anyString(), any(SlackPayload.class));
    }

    @Test
    @DisplayName("invalid menu(with 1 line) should not send message")
    void shouldNotSendMessageWhenMenuIsInvalid2() throws IOException, InterruptedException {
        // Given
        MenuResponse invalidMenu1 = new MenuResponse(LocalDate.now(), List.of("oneMenu"));
        when(menuService.getTodayMenu(monday)).thenReturn(invalidMenu1);
        mockClock(monday);

        // when
        slackNotifyService.notifyTodayMenu();

        // then
        verify(slackMessageSender, never()).sendMessage(anyString(), anyString(), any(SlackPayload.class));
    }


    @Test
    @DisplayName("invalid menu(with 2 lines) should not send message")
    void shouldNotSendMessageWhenMenuIsInvalid3() throws IOException, InterruptedException {
        MenuResponse invalidMenu1 = new MenuResponse(LocalDate.now(), List.of("oneMenu", "twoMenu"));
        when(menuService.getTodayMenu(monday)).thenReturn(invalidMenu1);
        mockClock(monday);

        // when
        slackNotifyService.notifyTodayMenu();

        // then
        verify(slackMessageSender, never()).sendMessage(anyString(), anyString(), any(SlackPayload.class));
    }

    @Test
    @DisplayName("valid menu(with 3 lines) should send message")
    void shouldNotSendMessageWhenMenuIsInvalid4() throws IOException, InterruptedException {
        MenuResponse invalidMenu1 = new MenuResponse(LocalDate.now(), List.of("oneMenu", "twoMenu", "threeMenu"));
        when(menuService.getTodayMenu(monday)).thenReturn(invalidMenu1);
        mockSlackConfig();
        mockClock(monday);

        // when
        slackNotifyService.notifyTodayMenu();

        // then send message once
        verify(slackMessageSender, times(1)).sendMessage(anyString(), anyString(), any(SlackPayload.class));
    }

    private void mockSlackConfig() {
        when(slackConfig.getSlackUrl()).thenReturn("https://hooks");
        when(slackConfig.getSlackChannel()).thenReturn("foodbox");
        when(slackConfig.getUserName()).thenReturn("점심봇");
        when(slackConfig.getSlackToken()).thenReturn("");
    }

    @Test
    @DisplayName("Should notify today false if it is Saturday")
    void shouldNotifyFalse1() throws IOException, InterruptedException {
        // Given
        LocalDate date = LocalDate.of(2025, 3, 29); // Saturday
        mockClock(date);

        // When
        slackNotifyService.notifyTodayMenu();

        // Then
        verify(menuService, never()).getTodayMenu(date);
    }

    @Test
    @DisplayName("Should notify today false if it is Sunday")
    void shouldNotifyFalse2() throws IOException, InterruptedException {
        // Given
        LocalDate date = LocalDate.of(2025, 3, 30); // SUNDAY
        mockClock(date);

        // When
        slackNotifyService.notifyTodayMenu();

        // Then
        verify(menuService, never()).getTodayMenu(date);
    }

    @Test
    @DisplayName("Should notify today false if it is Wednesday")
    void shouldNotifyFalse3() throws IOException, InterruptedException {
        // Given
        LocalDate date = LocalDate.of(2025, 3, 26); // WEDNESDAY
        mockClock(date);

        // When
        slackNotifyService.notifyTodayMenu();

        // Then
        verify(menuService, never()).getTodayMenu(date);
    }

    @Test
    @DisplayName("Should notify on [Monday], Tuesday, Thursday, Friday")
    void shouldNotifyTrue() throws IOException, InterruptedException {
        // Given
        LocalDate mon = LocalDate.of(2025, 3, 24);
        mockClock(mon);
        when(menuService.getTodayMenu(any())).thenReturn(new MenuResponse(LocalDate.now(), List.of("invalidMenu")));
        slackNotifyService.notifyTodayMenu();
        verify(menuService, only()).getTodayMenu(mon);
    }

    @Test
    @DisplayName("Should notify on Monday, [Tuesday], Thursday, Friday")
    void shouldNotifyTrue2() throws IOException, InterruptedException {
        // Given
        LocalDate tue = LocalDate.of(2025, 3, 25);
        mockClock(tue);
        when(menuService.getTodayMenu(any())).thenReturn(new MenuResponse(LocalDate.now(), List.of("invalidMenu")));
        slackNotifyService.notifyTodayMenu();
        verify(menuService, only()).getTodayMenu(tue);
    }

    @Test
    @DisplayName("Should notify on Monday, Tuesday, [Thursday], Friday")
    void shouldNotifyTrue3() throws IOException, InterruptedException {
        // Given
        LocalDate thu = LocalDate.of(2025, 3, 27);
        mockClock(thu);
        when(menuService.getTodayMenu(any())).thenReturn(new MenuResponse(LocalDate.now(), List.of("invalidMenu")));
        slackNotifyService.notifyTodayMenu();
        verify(menuService, only()).getTodayMenu(thu);
    }

    @Test
    @DisplayName("Should notify on Monday, Tuesday, Thursday, [Friday]")
    void shouldNotifyTrue4() throws IOException, InterruptedException {
        // Given
        LocalDate fri = LocalDate.of(2025, 3, 28);
        mockClock(fri);
        when(menuService.getTodayMenu(any())).thenReturn(new MenuResponse(LocalDate.now(), List.of("invalidMenu")));
        slackNotifyService.notifyTodayMenu();
        verify(menuService, only()).getTodayMenu(fri);
    }


}
