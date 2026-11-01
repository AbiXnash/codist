package com.demo.library.report;

import java.util.List;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class ReportController {
    private final ReportService service;

    public ReportController(ReportService service) {
        this.service = service;
    }

    @GetMapping("/api/reports/open-loans")
    public List<ReportService.Line> openLoans() {
        return service.openLoansPerBook();
    }
}
