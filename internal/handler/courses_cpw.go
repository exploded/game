package handler

// Rob's video courses from the Consortium Private Wealth site
// (consortiumpw.com.au/elearning, WordPress with LearnDash), copied once on
// 2026-10-30. The videos stay on Vimeo and YouTube. Titles, summaries and
// transcript links come from the course and lesson pages, and lengths from
// Vimeo and YouTube. Admins change the courses at /admin/courses.

// importCPWCourses adds the courses, published, with published_at set so
// publishing again never offers the new-course email.
const importCPWCourses = `INSERT OR IGNORE INTO courses (slug, title, summary, published, published_at, sort_order) VALUES
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 'How to invest: Secrets learnt from 20+ years in finance',
		'Presented by Rob Goudie, CFP, Grad Dip FP.

For the past 20+ years Rob has been working with superannuation investors, small business owners (including farmers) and direct share enthusiasts who love having control and input as to how their money is invested for their future.

BTW… In case you’re wondering, no. There are no “get rich quick” schemes here, the information included is based on commonsense that gets results. No fluff, no hype.

Tune in to hear the investing secrets Rob has learnt over the past two decades …',
		1, datetime('now'), 1),
	('how-to-pay-off-your-home-loan-faster', 'How to pay off your Home Loan FASTER!',
		'Presented by Rob Goudie, CFP, Grad Dip FP.

Live a life where you are in control of your finances so you can focus on what’s really important to you!

We all have that dream of living debt free and enjoying life to the maximum. With the number of borrowers behind in their mortgage repayments growing year on year (despite low interest rates), it’s vitally important to be in control of your home loan and increase the time you have to save for retirement.

To ensure you’re ahead of the game and have your mortgage under control, listen in to learn from the experts and have your home loan paid off years in advance. Allowing you to enjoy being debt free and, most importantly, saving you $10,000’s!',
		1, datetime('now'), 2),
	('beating-mortgage-stress', 'Beating Mortgage Stress',
		'Presented by Rob Goudie, CFP, Grad Dip FP.

Beating Mortgage Stress: How to recognise it and how to fix it!

With the number of borrowers behind in their mortgage repayments growing year on year (despite low interest rates), it’s vitally important to recognise mortgage stress and be able to withstand the strain skyrocketing living costs, personal loans and credit card liabilities can place on a family.

To ensure you’re ahead of the game and have your mortgage under control, listen in to learn from the experts and have your home loan paid off years in advance. Beating mortgage stress but, most importantly, saving you $10,000’s!',
		1, datetime('now'), 3),
	('investing-in-the-right-asset-class', 'Investing In The Right Asset Class',
		'Presented by Peter Thornhill.

“Absolute gold” for determining where to invest. – Rob Goudie.

Wanting to understand the best areas to invest to get the best results for your savings? This is for you!

Listen in to hear Rob’s investment mentor Peter Thornhill explain where to invest and why to be there. This course will make your investment decisions so much easier! You will know you are investing in the right areas of the share market to ensure the best result for you.

Peter has allowed us to put these presentations together in an easy to use course format for you – Thank you Peter!

Peter Thornhill’s Motivated Money website: https://www.motivatedmoney.com.au/',
		1, datetime('now'), 4),
	('beginners-guide-to-super', 'Beginners Guide to Super',
		'There are many parts to Australia’s superannuation system, but with a basic understanding it all begins to make perfect sense and the earlier you understand it, you’ll have confidence to utilise it to save plenty of tax and make $10,000’s for your retirement.

In this beginner’s guide, we’ll give you an overview of the rules and tell you why super is a smart way to save, the tax benefits, and how to put yourself in a strong financial position come retirement.

This course is designed for anyone looking to gain a basic, practical knowledge of the superannuation system within Australia. Sound intimidating? Don’t worry! This course is designed with complete beginners in mind. No previous personal finance knowledge (or math!) is required.

As for how this course is structured, we’ve designed it to maximise your enjoyment and engagement. That’s why we teach you an entire personal finance curriculum through a series of short, engaging videos. Each video teaches you everything you need to know (practically-speaking) about a specific topic, like superannuation, in a way that even a total beginner can understand.

Finally, as for your presenter, Rob has been a Financial Planner and Specialist Self Managed Super Fund Adviser for over 20 years and loves offering common sense solutions to financial problems in the most effective way possible. So what are you waiting for?',
		1, datetime('now'), 5),
	('how-super-works', 'How Super Works',
		'It’s never too early to start thinking about your super. Superannuation, or ‘super’, is money saved now, for you to enjoy in your retirement.

Your employer should pay 9.5% of your salary into a super fund, through the Superannuation Guarantee (SG). You can also top up your super by making your own contributions, and where you are eligible the government may add to it through co-contributions and the low income super contribution. Different super contributions can make and/or save you money.

In this course, we’ll tell you why super is a smart way to save, the tax benefits, and how to put yourself in a strong financial position come retirement.

This course is designed for anyone looking to gain an excellent, practical knowledge of superannuation contributions. Sound intimidating? Don’t worry! This course is designed with complete beginners in mind. No previous personal finance knowledge (or math!) is required.

As for how this course is structured, we’ve designed it to maximise your enjoyment and engagement. That’s why we teach you an entire personal finance curriculum through a series of short, engaging videos. Each video teaches you everything you need to know (practically-speaking) about a specific topic, like superannuation, in a way that even a total beginner can understand.

Finally, as for your presenter, Rob has been a Financial Planner and Specialist Self Managed Super Fund Adviser for over 20 years and loves offering common sense solutions to financial problems in the most effective way possible. So what are you waiting for?',
		1, datetime('now'), 6),
	('understanding-credit-cards', 'Understanding Credit Cards',
		'So… one of the biggest mistakes we can make is using a credit card without really understanding how it works. Credit cards can save you money, or they can completely ruin your finances, it all depends on how you use them.

In this course, we’ll tell you what you need to know to ensure you never let your credit card get the best of you!

This course is designed for anyone looking to gain an excellent, practical knowledge of credit cards. Sound intimidating? Don’t worry! This course is designed with complete beginners in mind. No previous personal finance knowledge (or math!) is required.

As for how this course is structured, we’ve designed it to maximise your enjoyment and engagement. That’s why we teach you an entire personal finance curriculum through a series of short, engaging videos. Each video teaches you everything you need to know (practically-speaking) about a specific topic, like credit cards, in a way that even a total beginner can understand.

Finally, as for your presenter, Rob has been a Financial Planner and Specialist Self Managed Super Fund Adviser for over 20 years and loves offering common sense solutions to financial problems in the most effective way possible. So what are you waiting for?',
		1, datetime('now'), 7)`

// importCPWLessons adds each course's lessons, unless the course already has
// some.
const importCPWLessons = `WITH v(slug, sort_order, title, description, video_url, embed_url, minutes) AS (VALUES
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 1, 'Welcome', '', 'https://vimeo.com/411280523', 'https://player.vimeo.com/video/411280523', 13),
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 2, 'The convenience of managed funds can come at a price', '', 'https://vimeo.com/411280762', 'https://player.vimeo.com/video/411280762', 12),
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 3, 'Would you recognise conflicted advice?', '', 'https://vimeo.com/411280995', 'https://player.vimeo.com/video/411280995', 10),
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 4, 'Who sits between you and your investments', '', 'https://vimeo.com/411281210', 'https://player.vimeo.com/video/411281210', 8),
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 5, 'Fees are impacting your retirement savings', '', 'https://vimeo.com/411281351', 'https://player.vimeo.com/video/411281351', 11),
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 6, 'Find out how we select our share recommendations', '', 'https://vimeo.com/411281643', 'https://player.vimeo.com/video/411281643', 7),
	('how-to-invest-secrets-learnt-from-20-years-in-finance', 7, 'Want to hear our own headache confession?', '', 'https://vimeo.com/411281808', 'https://player.vimeo.com/video/411281808', 8),
	('how-to-pay-off-your-home-loan-faster', 1, 'Introduction', '', 'https://vimeo.com/386344807', 'https://player.vimeo.com/video/386344807', 3),
	('how-to-pay-off-your-home-loan-faster', 2, 'What will you achieve?', '', 'https://vimeo.com/386345902', 'https://player.vimeo.com/video/386345902', 6),
	('how-to-pay-off-your-home-loan-faster', 3, 'What needs to be done to pay your Home Loan off quicker', '', 'https://vimeo.com/386346679', 'https://player.vimeo.com/video/386346679', 5),
	('how-to-pay-off-your-home-loan-faster', 4, 'Assessing your current Home Loan', '', 'https://vimeo.com/386348675', 'https://player.vimeo.com/video/386348675', 4),
	('how-to-pay-off-your-home-loan-faster', 5, 'How old will you be when you’re debt free?', '', 'https://vimeo.com/386349239', 'https://player.vimeo.com/video/386349239', 9),
	('how-to-pay-off-your-home-loan-faster', 6, 'Let’s make some interest + repayment changes', '', 'https://vimeo.com/386670100', 'https://player.vimeo.com/video/386670100', 6),
	('how-to-pay-off-your-home-loan-faster', 7, 'Refinancing and bringing the interest rate down', '', 'https://vimeo.com/386670681', 'https://player.vimeo.com/video/386670681', 5),
	('how-to-pay-off-your-home-loan-faster', 8, 'What you are looking for in a loan', '', 'https://vimeo.com/386670943', 'https://player.vimeo.com/video/386670943', 4),
	('how-to-pay-off-your-home-loan-faster', 9, 'But I’ve got multiple debts', '', 'https://vimeo.com/386671080', 'https://player.vimeo.com/video/386671080', 7),
	('how-to-pay-off-your-home-loan-faster', 10, 'Need incentive to pay your loan off?', '', 'https://vimeo.com/386915510', 'https://player.vimeo.com/video/386915510', 3),
	('how-to-pay-off-your-home-loan-faster', 11, 'Saving money and budgeting', '', 'https://vimeo.com/386915679', 'https://player.vimeo.com/video/386915679', 4),
	('how-to-pay-off-your-home-loan-faster', 12, 'How to increase repayments and use superannuation to help', '', 'https://vimeo.com/386916133', 'https://player.vimeo.com/video/386916133', 4),
	('how-to-pay-off-your-home-loan-faster', 13, 'What if the figures don’t look so good?', '', 'https://vimeo.com/386916343', 'https://player.vimeo.com/video/386916343', 3),
	('how-to-pay-off-your-home-loan-faster', 14, 'Take control and the no nos', '', 'https://vimeo.com/386916475', 'https://player.vimeo.com/video/386916475', 2),
	('how-to-pay-off-your-home-loan-faster', 15, 'The Superannuation opportunity cost', '', 'https://vimeo.com/386916597', 'https://player.vimeo.com/video/386916597', 5),
	('how-to-pay-off-your-home-loan-faster', 16, 'Get started', '', 'https://vimeo.com/386916762', 'https://player.vimeo.com/video/386916762', 3),
	('beating-mortgage-stress', 1, 'Introduction', '', 'https://vimeo.com/353491727', 'https://player.vimeo.com/video/353491727', 7),
	('beating-mortgage-stress', 2, 'The definition of Mortgage Stress', '', 'https://vimeo.com/353491701', 'https://player.vimeo.com/video/353491701', 6),
	('beating-mortgage-stress', 3, 'Debt is cheap and easy', '', 'https://vimeo.com/353491678', 'https://player.vimeo.com/video/353491678', 8),
	('beating-mortgage-stress', 4, 'Why care about Mortgage Stress?', '', 'https://vimeo.com/353491668', 'https://player.vimeo.com/video/353491668', 3),
	('beating-mortgage-stress', 5, 'Do you have Mortgage Stress?', '', 'https://vimeo.com/353940630', 'https://player.vimeo.com/video/353940630', 3),
	('beating-mortgage-stress', 6, 'What if the bank is on your back?', '', 'https://vimeo.com/353939996', 'https://player.vimeo.com/video/353939996', 5),
	('beating-mortgage-stress', 7, 'What if paying your loans has been an ongoing issue?', '', 'https://vimeo.com/353493527', 'https://player.vimeo.com/video/353493527', 4),
	('beating-mortgage-stress', 8, 'Lender is threatening legal action', '', 'https://vimeo.com/353495824', 'https://player.vimeo.com/video/353495824', 2),
	('beating-mortgage-stress', 9, 'Time to get moving in the right direction', '', 'https://vimeo.com/353495807', 'https://player.vimeo.com/video/353495807', 3),
	('beating-mortgage-stress', 10, 'Time to track your spending', '', 'https://vimeo.com/353941082', 'https://player.vimeo.com/video/353941082', 6),
	('beating-mortgage-stress', 11, 'Avoiding Bill Shock', '', 'https://vimeo.com/353693866', 'https://player.vimeo.com/video/353693866', 4),
	('beating-mortgage-stress', 12, 'Save money and avoid impulse buying', '', 'https://vimeo.com/353695152', 'https://player.vimeo.com/video/353695152', 6),
	('beating-mortgage-stress', 13, 'But the budget is already stretched', '', 'https://vimeo.com/353941184', 'https://player.vimeo.com/video/353941184', 3),
	('beating-mortgage-stress', 14, 'Comparing home loan rates and refinancing', '', 'https://vimeo.com/355262920', 'https://player.vimeo.com/video/355262920', 6),
	('beating-mortgage-stress', 15, 'Loan features & comparison rates', '', 'https://vimeo.com/353698832', 'https://player.vimeo.com/video/353698832', 6),
	('beating-mortgage-stress', 16, 'What type of loan is best for you?', '', 'https://vimeo.com/353942071', 'https://player.vimeo.com/video/353942071', 7),
	('beating-mortgage-stress', 17, 'But I have multiple debts', '', 'https://vimeo.com/355262820', 'https://player.vimeo.com/video/355262820', 10),
	('beating-mortgage-stress', 18, 'Need incentive to pay off debt?', '', 'https://vimeo.com/356106954', 'https://player.vimeo.com/video/356106954', 3),
	('beating-mortgage-stress', 19, 'One way to reduce expenses', '', 'https://vimeo.com/357454295', 'https://player.vimeo.com/video/357454295', 4),
	('beating-mortgage-stress', 20, 'Are there complexities?', '', 'https://vimeo.com/357454284', 'https://player.vimeo.com/video/357454284', 5),
	('beating-mortgage-stress', 21, 'When can you access your superannuation?', '', 'https://vimeo.com/357454267', 'https://player.vimeo.com/video/357454267', 5),
	('beating-mortgage-stress', 22, 'Early release of superannuation', '', 'https://vimeo.com/357454245', 'https://player.vimeo.com/video/357454245', 6),
	('beating-mortgage-stress', 23, 'How to calculate when your loan will be paid off', '', 'https://vimeo.com/357454222', 'https://player.vimeo.com/video/357454222', 9),
	('beating-mortgage-stress', 24, 'What if the loan figures don’t look so good?', '', 'https://vimeo.com/357454202', 'https://player.vimeo.com/video/357454202', 3),
	('beating-mortgage-stress', 25, 'Is there an opportunity cost?', '', 'https://vimeo.com/357712618', 'https://player.vimeo.com/video/357712618', 6),
	('beating-mortgage-stress', 26, 'Selling and renting', '', 'https://vimeo.com/363724956', 'https://player.vimeo.com/video/363724956', 7),
	('beating-mortgage-stress', 27, 'How can you increase repayments?', '', 'https://vimeo.com/363724921', 'https://player.vimeo.com/video/363724921', 4),
	('beating-mortgage-stress', 28, 'The tough question', '', 'https://vimeo.com/363725068', 'https://player.vimeo.com/video/363725068', 2),
	('beating-mortgage-stress', 29, 'Taking control, taking action and the stuff to avoid', '', 'https://vimeo.com/363725043', 'https://player.vimeo.com/video/363725043', 4),
	('beating-mortgage-stress', 30, 'Start now', '', 'https://vimeo.com/363725009', 'https://player.vimeo.com/video/363725009', 3),
	('investing-in-the-right-asset-class', 1, 'Introduction', '', 'https://vimeo.com/352399294', 'https://player.vimeo.com/video/352399294', 4),
	('investing-in-the-right-asset-class', 2, 'A Rational Sharemarket', '', 'https://www.youtube.com/watch?v=7Qja3GHefiE', 'https://www.youtube-nocookie.com/embed/7Qja3GHefiE?rel=0', 9),
	('investing-in-the-right-asset-class', 3, 'Behaviour Finance – A Personal Perception', '', 'https://www.youtube.com/watch?v=Bin_9svYcjI', 'https://www.youtube-nocookie.com/embed/Bin_9svYcjI?rel=0', 14),
	('investing-in-the-right-asset-class', 4, 'Introduction to Income', '', 'https://www.youtube.com/watch?v=0HnHSeOeK7I', 'https://www.youtube-nocookie.com/embed/0HnHSeOeK7I?rel=0', 9),
	('investing-in-the-right-asset-class', 5, 'Recent Events', '', 'https://www.youtube.com/watch?v=2b8PD27y_wI', 'https://www.youtube-nocookie.com/embed/2b8PD27y_wI?rel=0', 5),
	('investing-in-the-right-asset-class', 6, 'The Yield Trap', '', 'https://www.youtube.com/watch?v=cpVSmgZRHA8', 'https://www.youtube-nocookie.com/embed/cpVSmgZRHA8?rel=0', 6),
	('investing-in-the-right-asset-class', 7, 'Property in the Investment Equation', '', 'https://www.youtube.com/watch?v=ppOXmWW4lZ4', 'https://www.youtube-nocookie.com/embed/ppOXmWW4lZ4?rel=0', 6),
	('investing-in-the-right-asset-class', 8, 'Recurring Themes', '', 'https://www.youtube.com/watch?v=kLKzI-zZadY', 'https://www.youtube-nocookie.com/embed/kLKzI-zZadY?rel=0', 6),
	('beginners-guide-to-super', 1, 'Introduction', '', 'https://vimeo.com/278415196', 'https://player.vimeo.com/video/278415196', 4),
	('beginners-guide-to-super', 2, 'What Super is and isn’t', '', 'https://vimeo.com/278423725', 'https://player.vimeo.com/video/278423725', 4),
	('beginners-guide-to-super', 3, 'What are the tax rates', '', 'https://vimeo.com/279371622', 'https://player.vimeo.com/video/279371622', 5),
	('beginners-guide-to-super', 4, 'Who can contribute', '', 'https://vimeo.com/279379211', 'https://player.vimeo.com/video/279379211', 6),
	('beginners-guide-to-super', 5, 'How your fund grows', '', 'https://vimeo.com/279386361', 'https://player.vimeo.com/video/279386361', 4),
	('beginners-guide-to-super', 6, 'Tax savings', '', 'https://vimeo.com/279391170', 'https://player.vimeo.com/video/279391170', 7),
	('beginners-guide-to-super', 7, 'Compounding interest', '', 'https://vimeo.com/279392878', 'https://player.vimeo.com/video/279392878', 6),
	('beginners-guide-to-super', 8, 'Insurance within Super', '', 'https://vimeo.com/279394824', 'https://player.vimeo.com/video/279394824', 4),
	('beginners-guide-to-super', 9, 'When can I access my Super', '', 'https://vimeo.com/279587683', 'https://player.vimeo.com/video/279587683', 3),
	('beginners-guide-to-super', 10, 'When I die', '', 'https://vimeo.com/279594354', 'https://player.vimeo.com/video/279594354', 5),
	('beginners-guide-to-super', 11, 'Types of funds', '', 'https://vimeo.com/279597641', 'https://player.vimeo.com/video/279597641', 5),
	('how-super-works', 1, 'Introduction', '', 'https://vimeo.com/269092939', 'https://player.vimeo.com/video/269092939', 4),
	('how-super-works', 2, 'Why Superannuation', '', 'https://vimeo.com/269094351', 'https://player.vimeo.com/video/269094351', 5),
	('how-super-works', 3, 'Superannuation contribution types', '', 'https://vimeo.com/269094386', 'https://player.vimeo.com/video/269094386', 2),
	('how-super-works', 4, 'Concessional superannuation contributions', '', 'https://vimeo.com/269094407', 'https://player.vimeo.com/video/269094407', 6),
	('how-super-works', 5, 'Non Concessional superannuation contributions', '', 'https://vimeo.com/269094448', 'https://player.vimeo.com/video/269094448', 4),
	('how-super-works', 6, 'Spouse contributions', '', 'https://vimeo.com/269094475', 'https://player.vimeo.com/video/269094475', 3),
	('how-super-works', 7, 'Co-contributions', '', 'https://vimeo.com/269094490', 'https://player.vimeo.com/video/269094490', 9),
	('how-super-works', 8, 'The work test', '', 'https://vimeo.com/269094550', 'https://player.vimeo.com/video/269094550', 3),
	('how-super-works', 9, 'New rules for downsizers', '', 'https://vimeo.com/269094587', 'https://player.vimeo.com/video/269094587', 3),
	('how-super-works', 10, 'Small Business CGT concessions', '', 'https://vimeo.com/269094624', 'https://player.vimeo.com/video/269094624', 4),
	('how-super-works', 11, 'Summary', '', 'https://vimeo.com/269094658', 'https://player.vimeo.com/video/269094658', 2),
	('understanding-credit-cards', 1, 'Introduction', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-1-Introduction.pdf', 'https://vimeo.com/261244810', 'https://player.vimeo.com/video/261244810', 3),
	('understanding-credit-cards', 2, 'Why this topic', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-2-Why-this-topic.pdf', 'https://vimeo.com/261244760', 'https://player.vimeo.com/video/261244760', 4),
	('understanding-credit-cards', 3, 'So much debt', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-3-So-much-debt.pdf', 'https://vimeo.com/261244833', 'https://player.vimeo.com/video/261244833', 7),
	('understanding-credit-cards', 4, 'Convenience and limit increases', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-4-Convenience-and-limit-increases.pdf', 'https://vimeo.com/261244900', 'https://player.vimeo.com/video/261244900', 4),
	('understanding-credit-cards', 5, '9 year loan', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-5-9-year-loan.pdf', 'https://vimeo.com/261244939', 'https://player.vimeo.com/video/261244939', 3),
	('understanding-credit-cards', 6, 'What should you do', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-6-What-should-you-do.pdf', 'https://vimeo.com/261244951', 'https://player.vimeo.com/video/261244951', 3),
	('understanding-credit-cards', 7, 'Calculate paying off your debt', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-7-Calculate-paying-off-you-debt.pdf', 'https://vimeo.com/261244980', 'https://player.vimeo.com/video/261244980', 4),
	('understanding-credit-cards', 8, 'Master Debt 1', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-8-Master-Debt-1.pdf', 'https://vimeo.com/261244994', 'https://player.vimeo.com/video/261244994', 7),
	('understanding-credit-cards', 9, 'Master Debt 2', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-8-Master-Debt-2.pdf', 'https://vimeo.com/261245032', 'https://player.vimeo.com/video/261245032', 4),
	('understanding-credit-cards', 10, 'Alternatives to Credit Cards', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-9-Alternatives-to-Credit-Cards.pdf', 'https://vimeo.com/261245058', 'https://player.vimeo.com/video/261245058', 3),
	('understanding-credit-cards', 11, 'Summary', 'Lesson transcript (PDF): https://www.consortiumpw.com.au/wp-content/uploads/2018/05/Video-10-Summary.pdf', 'https://vimeo.com/261245069', 'https://player.vimeo.com/video/261245069', 2))
INSERT INTO lessons (course_id, title, description, video_url, embed_url, minutes, sort_order)
SELECT c.id, v.title, v.description, v.video_url, v.embed_url, v.minutes, v.sort_order
FROM v JOIN courses c ON c.slug = v.slug
WHERE NOT EXISTS (SELECT 1 FROM lessons l WHERE l.course_id = c.id)`
