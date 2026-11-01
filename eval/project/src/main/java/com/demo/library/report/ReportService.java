package com.demo.library.report;

import com.demo.library.book.Book;
import com.demo.library.book.BookRepository;
import com.demo.library.loan.Loan;
import com.demo.library.loan.LoanRepository;
import java.util.ArrayList;
import java.util.List;
import org.springframework.stereotype.Service;

@Service
public class ReportService {
    private final LoanRepository loans;
    private final BookRepository books;

    public ReportService(LoanRepository loans, BookRepository books) {
        this.loans = loans;
        this.books = books;
    }

    public record Line(String title, long openLoans) {
    }

    public List<Line> openLoansPerBook() {
        List<Line> lines = new ArrayList<>();
        for (Book book : books.findAll()) {
            long open = 0;
            for (Loan loan : loans.findByReturnedFalse()) {
                if (loan.getBookId() == book.getId()) {
                    open++;
                }
            }
            lines.add(new Line(book.getTitle(), open));
        }
        return lines;
    }
}
